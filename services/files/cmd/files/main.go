package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
	"github.com/sk1fy/team-os-backend/services/files/internal/application"
	"github.com/sk1fy/team-os-backend/services/files/internal/config"
	"github.com/sk1fy/team-os-backend/services/files/internal/objectstore"
	"github.com/sk1fy/team-os-backend/services/files/internal/storage"
	transport "github.com/sk1fy/team-os-backend/services/files/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "healthcheck" {
		if err := healthcheck(os.Args[2]); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("files stopped", "error", err)
		os.Exit(1)
	}
}

func healthcheck(url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("healthcheck вернул %s", response.Status)
	}
	return nil
}

func run(logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	shutdownTelemetry, err := httpx.SetupTelemetry("files")
	if err != nil {
		return fmt.Errorf("настроить телеметрию: %w", err)
	}
	defer func() {
		if shutdownErr := shutdownTelemetry(); shutdownErr != nil {
			logger.Error("shutdown telemetry", "error", shutdownErr)
		}
	}()
	var verifier *sharedauth.TokenVerifier
	if c.JWTPublicKey != "" {
		key, parseErr := sharedauth.ParsePublicKey(c.JWTPublicKey)
		if parseErr != nil {
			return fmt.Errorf("FILES_JWT_PUBLIC_KEY: %w", parseErr)
		}
		verifier = sharedauth.NewTokenVerifier(key, c.JWTIssuer, c.JWTAudience)
	}
	pool, err := pgxpool.New(context.Background(), c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("подключиться к PostgreSQL: %w", err)
	}
	defer pool.Close()
	startup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = pool.Ping(startup); err != nil {
		return fmt.Errorf("проверить PostgreSQL: %w", err)
	}
	objects, err := objectstore.New(c.S3Endpoint, c.S3PublicEndpoint, c.S3AccessKey, c.S3SecretKey, c.S3Region, c.S3Bucket, c.S3Secure)
	if err != nil {
		return fmt.Errorf("создать S3-клиент: %w", err)
	}
	if err = objects.EnsureBucket(startup, c.S3CreateBucket, c.S3Region); err != nil {
		return fmt.Errorf("проверить S3: %w", err)
	}
	service := application.New(storage.New(pool), objects, c.MaxFileSize, c.AllowedTypes, c.DownloadURLTTL)
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", c.GRPCAddr)
	if err != nil {
		return fmt.Errorf("слушать gRPC: %w", err)
	}
	g := grpc.NewServer(grpc.MaxRecvMsgSize((1<<20)+(64<<10)), grpc.MaxSendMsgSize(1<<20))
	filesv1.RegisterFilesServiceServer(g, transport.New(service, verifier, c.TrustedMetadata, c.TempDir))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	healthv1.RegisterHealthServer(g, healthServer)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", httpx.MetricsHandler())
	mux.Handle("GET /readyz", httpx.Readyz(map[string]httpx.ReadinessCheck{
		"postgres": func(ctx context.Context) error {
			check, done := context.WithTimeout(ctx, time.Second)
			defer done()
			return pool.Ping(check)
		},
		"s3": func(ctx context.Context) error {
			check, done := context.WithTimeout(ctx, time.Second)
			defer done()
			return objects.Ready(check)
		},
	}))
	httpServer := &http.Server{Addr: c.HTTPAddr, Handler: httpx.Chain(mux, httpx.RequestID, httpx.Recoverer(logger), httpx.Tracing("files"), httpx.Metrics, httpx.Logging(logger)), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	errs := make(chan error, 2)
	go func() { logger.Info("files gRPC server started", "address", c.GRPCAddr); errs <- g.Serve(listener) }()
	go func() {
		logger.Info("files HTTP server started", "address", c.HTTPAddr)
		errs <- httpServer.ListenAndServe()
	}()
	signalContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var runErr error
	select {
	case <-signalContext.Done():
	case runErr = <-errs:
		if errors.Is(runErr, http.ErrServerClosed) || errors.Is(runErr, grpc.ErrServerStopped) {
			runErr = nil
		}
	}
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)
	shutdown, done := context.WithTimeout(context.Background(), c.ShutdownTimeout)
	defer done()
	_ = httpServer.Shutdown(shutdown)
	stopped := make(chan struct{})
	go func() { g.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-shutdown.Done():
		g.Stop()
		if runErr == nil {
			runErr = errors.New("истёк тайм-аут остановки gRPC")
		}
	}
	return runErr
}
