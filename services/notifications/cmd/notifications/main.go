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
	notificationsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/notifications/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/application"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/config"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/consumers"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/deliverycrypto"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/mailer"
	transport "github.com/sk1fy/team-os-backend/services/notifications/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("notifications stopped", "error", err)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	shutdownTelemetry, err := httpx.SetupTelemetry("notifications")
	if err != nil {
		return fmt.Errorf("настроить телеметрию: %w", err)
	}
	defer func() {
		if shutdownErr := shutdownTelemetry(); shutdownErr != nil {
			logger.Error("shutdown telemetry", "error", shutdownErr)
		}
	}()
	key, err := sharedauth.ParsePublicKey(c.JWTPublicKey)
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(context.Background(), c.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(context.Background()); err != nil {
		return err
	}
	bus, err := eventbus.Connect(c.NATSURL)
	if err != nil {
		return err
	}
	defer func() { _ = bus.Drain() }()
	service, err := application.New(pool)
	if err != nil {
		return err
	}
	emailSender, err := buildEmailSender(c, logger)
	if err != nil {
		return err
	}
	if err = service.SetEmailSender(emailSender); err != nil {
		return err
	}
	emailDecryptor, err := deliverycrypto.New(c.ExternalEmailKey, c.ExternalEmailKeyID)
	clear(c.ExternalEmailKey)
	if err != nil {
		return err
	}
	if err = service.SetExternalEmailDecryptor(emailDecryptor); err != nil {
		return err
	}
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = consumers.Start(root, bus, service, logger); err != nil {
		return err
	}
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", c.GRPCAddr)
	if err != nil {
		return err
	}
	g := grpc.NewServer()
	notificationsv1.RegisterNotificationsServiceServer(g, transport.New(service, sharedauth.NewTokenVerifier(key, c.JWTIssuer, c.JWTAudience)))
	h := health.NewServer()
	h.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	healthv1.RegisterHealthServer(g, h)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", httpx.MetricsHandler())
	mux.Handle("GET /readyz", httpx.Readyz(map[string]httpx.ReadinessCheck{"postgres": func(ctx context.Context) error { return pool.Ping(ctx) }, "nats": func(ctx context.Context) error { return bus.Ready(ctx) }}))
	httpServer := &http.Server{Addr: c.HTTPAddr, Handler: httpx.Chain(mux, httpx.RequestID, httpx.Recoverer(logger), httpx.Tracing("notifications"), httpx.Metrics, httpx.Logging(logger)), ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 2)
	go func() { errs <- g.Serve(listener) }()
	go func() { errs <- httpServer.ListenAndServe() }()
	sig, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-sig.Done():
	case err = <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancel()
	h.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)
	ctx, done := context.WithTimeout(context.Background(), c.ShutdownTimeout)
	defer done()
	_ = httpServer.Shutdown(ctx)
	g.GracefulStop()
	return err
}

func buildEmailSender(c config.Config, logger *slog.Logger) (mailer.Sender, error) {
	if c.EmailProvider == "smtp" {
		return mailer.NewSMTPSender(mailer.SMTPConfig{
			Host: c.SMTPHost, Port: c.SMTPPort, Username: c.SMTPUsername, Password: c.SMTPPassword,
			FromAddress: c.SMTPFrom, FromName: c.SMTPFromName, RequireTLS: c.SMTPRequireTLS,
		})
	}
	return mailer.NewLogSender(logger), nil
}

var _ = fmt.Errorf
