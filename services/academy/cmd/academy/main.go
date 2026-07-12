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
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"github.com/sk1fy/team-os-backend/services/academy/internal/clients"
	"github.com/sk1fy/team-os-backend/services/academy/internal/config"
	"github.com/sk1fy/team-os-backend/services/academy/internal/consumers"
	"github.com/sk1fy/team-os-backend/services/academy/internal/outbox"
	academygrpc "github.com/sk1fy/team-os-backend/services/academy/internal/transport/grpc"
	"github.com/sk1fy/team-os-backend/services/academy/internal/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
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
		logger.Error("academy stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	publicKey, err := sharedauth.ParsePublicKey(configuration.JWTPublicKey)
	if err != nil {
		return fmt.Errorf("ACADEMY_JWT_PUBLIC_KEY: %w", err)
	}
	verifier := sharedauth.NewTokenVerifier(publicKey, configuration.JWTIssuer, configuration.JWTAudience)

	poolConfig, err := pgxpool.ParseConfig(configuration.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse ACADEMY_DB_URL: %w", err)
	}
	poolConfig.MaxConns = 20
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()
	startupContext, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startupCancel()
	if err = pool.Ping(startupContext); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	bus, err := eventbus.Connect(configuration.NATSURL)
	if err != nil {
		return err
	}
	defer func() {
		if drainErr := bus.Drain(); drainErr != nil {
			logger.Error("NATS drain failed", "error", drainErr)
		}
	}()

	kbConnection, err := grpc.NewClient(
		configuration.KbGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to kb: %w", err)
	}
	defer func() {
		if closeErr := kbConnection.Close(); closeErr != nil {
			logger.Error("close kb gRPC connection", "error", closeErr)
		}
	}()
	companyConnection, err := grpc.NewClient(
		configuration.CompanyGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to company: %w", err)
	}
	defer func() {
		if closeErr := companyConnection.Close(); closeErr != nil {
			logger.Error("close company gRPC connection", "error", closeErr)
		}
	}()

	service, err := application.NewService(
		pool,
		clients.NewKb(kbv1.NewKbServiceClient(kbConnection)),
		clients.NewCompany(companyv1.NewCompanyServiceClient(companyConnection)),
		logger,
	)
	if err != nil {
		return fmt.Errorf("initialize academy application: %w", err)
	}

	rootContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	riverClient, err := workers.Setup(rootContext, pool, service)
	if err != nil {
		return fmt.Errorf("initialize river workers: %w", err)
	}
	if err = riverClient.Start(rootContext); err != nil {
		return fmt.Errorf("start river workers: %w", err)
	}
	defer func() {
		stopContext, stopCancel := context.WithTimeout(context.Background(), configuration.ShutdownTimeout)
		defer stopCancel()
		if stopErr := riverClient.Stop(stopContext); stopErr != nil {
			logger.Error("river stop failed", "error", stopErr)
		}
	}()

	if err = consumers.Start(rootContext, bus, service, logger); err != nil {
		return fmt.Errorf("start kb consumers: %w", err)
	}

	var listenConfig net.ListenConfig
	grpcListener, err := listenConfig.Listen(context.Background(), "tcp", configuration.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC: %w", err)
	}
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(4<<20),
		grpc.MaxSendMsgSize(4<<20),
	)
	academyv1.RegisterAcademyServiceServer(grpcServer, academygrpc.NewServer(service, verifier))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	httpRouter := http.NewServeMux()
	httpRouter.Handle("GET /healthz", httpx.Healthz())
	httpRouter.Handle("GET /readyz", httpx.Readyz(map[string]httpx.ReadinessCheck{
		"postgres": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return pool.Ping(checkContext)
		},
		"nats": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return bus.Ready(checkContext)
		},
	}))
	httpServer := &http.Server{
		Addr: configuration.HTTPAddr,
		Handler: httpx.Chain(
			httpRouter,
			httpx.RequestID,
			httpx.Recoverer(logger),
			httpx.Tracing("academy"),
			httpx.Logging(logger),
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErrors := make(chan error, 3)
	go func() {
		logger.Info("academy gRPC server started", "address", configuration.GRPCAddr)
		serverErrors <- grpcServer.Serve(grpcListener)
	}()
	go func() {
		logger.Info("academy HTTP server started", "address", configuration.HTTPAddr)
		serverErrors <- httpServer.ListenAndServe()
	}()
	go func() {
		serverErrors <- outbox.NewRelay(pool, bus, logger).Run(rootContext)
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var runErr error
	select {
	case <-signalContext.Done():
	case runErr = <-serverErrors:
		if errors.Is(runErr, http.ErrServerClosed) || errors.Is(runErr, grpc.ErrServerStopped) {
			runErr = nil
		}
	}
	cancel()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), configuration.ShutdownTimeout)
	defer shutdownCancel()
	if err = httpServer.Shutdown(shutdownContext); err != nil && runErr == nil {
		runErr = fmt.Errorf("shutdown HTTP: %w", err)
	}
	grpcStopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcStopped)
	}()
	select {
	case <-grpcStopped:
	case <-shutdownContext.Done():
		grpcServer.Stop()
		if runErr == nil {
			runErr = errors.New("gRPC graceful shutdown timed out")
		}
	}
	return runErr
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
		return fmt.Errorf("healthcheck returned %s", response.Status)
	}
	return nil
}
