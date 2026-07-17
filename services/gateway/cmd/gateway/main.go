package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	notificationsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/notifications/v1"
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/authmw"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/config"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/ratelimit"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
		logger.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	shutdownTelemetry, err := httpx.SetupTelemetry("gateway")
	if err != nil {
		return fmt.Errorf("настроить телеметрию: %w", err)
	}
	defer func() {
		if shutdownErr := shutdownTelemetry(); shutdownErr != nil {
			logger.Error("shutdown telemetry", "error", shutdownErr)
		}
	}()
	publicKey, err := sharedauth.ParsePublicKey(configuration.JWTPublicKey)
	if err != nil {
		return fmt.Errorf("GATEWAY_JWT_PUBLIC_KEY: %w", err)
	}
	verifier := sharedauth.NewTokenVerifier(publicKey, configuration.JWTIssuer, configuration.JWTAudience)
	companyConnection, err := grpc.NewClient(
		configuration.CompanyGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(defaultUnaryTimeout(5*time.Second)),
	)
	if err != nil {
		return fmt.Errorf("connect to company: %w", err)
	}
	defer func() {
		if closeErr := companyConnection.Close(); closeErr != nil {
			logger.Error("close company gRPC connection", "error", closeErr)
		}
	}()
	companyClient := companyv1.NewCompanyServiceClient(companyConnection)
	kbConnection, err := grpc.NewClient(
		configuration.KbGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(defaultUnaryTimeout(5*time.Second)),
	)
	if err != nil {
		return fmt.Errorf("connect to kb: %w", err)
	}
	defer func() {
		if closeErr := kbConnection.Close(); closeErr != nil {
			logger.Error("close kb gRPC connection", "error", closeErr)
		}
	}()
	kbClient := kbv1.NewKbServiceClient(kbConnection)
	tasksConnection, err := grpc.NewClient(
		configuration.TasksGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(defaultUnaryTimeout(5*time.Second)),
	)
	if err != nil {
		return fmt.Errorf("connect to tasks: %w", err)
	}
	defer func() {
		if closeErr := tasksConnection.Close(); closeErr != nil {
			logger.Error("close tasks gRPC connection", "error", closeErr)
		}
	}()
	tasksClient := tasksv1.NewTasksServiceClient(tasksConnection)
	academyConnection, err := grpc.NewClient(
		configuration.AcademyGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(defaultUnaryTimeout(5*time.Second)),
	)
	if err != nil {
		return fmt.Errorf("connect to academy: %w", err)
	}
	defer func() {
		if closeErr := academyConnection.Close(); closeErr != nil {
			logger.Error("close academy gRPC connection", "error", closeErr)
		}
	}()
	academyClient := academyv1.NewAcademyServiceClient(academyConnection)
	notificationsConnection, err := grpc.NewClient(configuration.NotificationsGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(defaultUnaryTimeout(5*time.Second)))
	if err != nil {
		return fmt.Errorf("connect to notifications: %w", err)
	}
	defer func() {
		if closeErr := notificationsConnection.Close(); closeErr != nil {
			logger.Error("close notifications gRPC connection", "error", closeErr)
		}
	}()
	notificationsClient := notificationsv1.NewNotificationsServiceClient(notificationsConnection)
	filesConnection, err := grpc.NewClient(
		configuration.FilesGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(defaultUnaryTimeout(5*time.Second)),
	)
	if err != nil {
		return fmt.Errorf("connect to files: %w", err)
	}
	defer func() {
		if closeErr := filesConnection.Close(); closeErr != nil {
			logger.Error("close files gRPC connection", "error", closeErr)
		}
	}()
	filesClient := filesv1.NewFilesServiceClient(filesConnection)
	handler := transport.NewHandler(
		companyClient,
		kbClient,
		tasksClient,
		academyClient,
		transport.CookieConfig{Secure: configuration.CookieSecure},
		logger,
		notificationsClient,
	)
	handler.SetFilesClient(filesClient)

	router := chi.NewRouter()
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   configuration.CORSOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", httpx.RequestIDHeader, "If-Match"},
		ExposedHeaders:   []string{httpx.RequestIDHeader},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	router.Use(httpx.RequestID, httpx.Recoverer(logger), httpx.Tracing("gateway"), httpx.Metrics, httpx.Logging(logger))
	router.Get("/healthz", httpx.Healthz().ServeHTTP)
	router.Get("/metrics", httpx.MetricsHandler().ServeHTTP)
	companyHealthClient := grpc_health_v1.NewHealthClient(companyConnection)
	kbHealthClient := grpc_health_v1.NewHealthClient(kbConnection)
	tasksHealthClient := grpc_health_v1.NewHealthClient(tasksConnection)
	academyHealthClient := grpc_health_v1.NewHealthClient(academyConnection)
	notificationsHealthClient := grpc_health_v1.NewHealthClient(notificationsConnection)
	filesHealthClient := grpc_health_v1.NewHealthClient(filesConnection)
	router.Get("/readyz", httpx.Readyz(map[string]httpx.ReadinessCheck{
		"files": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			response, checkErr := filesHealthClient.Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
			if checkErr != nil {
				return checkErr
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return errors.New("files is not serving")
			}
			return nil
		},
		"academy": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			response, checkErr := academyHealthClient.Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
			if checkErr != nil {
				return checkErr
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return errors.New("academy is not serving")
			}
			return nil
		},
		"notifications": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			response, checkErr := notificationsHealthClient.Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
			if checkErr != nil {
				return checkErr
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return errors.New("notifications is not serving")
			}
			return nil
		},
		"company": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			response, checkErr := companyHealthClient.Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
			if checkErr != nil {
				return checkErr
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return errors.New("company is not serving")
			}
			return nil
		},
		"kb": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			response, checkErr := kbHealthClient.Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
			if checkErr != nil {
				return checkErr
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return errors.New("kb is not serving")
			}
			return nil
		},
		"tasks": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			response, checkErr := tasksHealthClient.Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
			if checkErr != nil {
				return checkErr
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return errors.New("tasks is not serving")
			}
			return nil
		},
	}).ServeHTTP)
	router.Group(func(apiRouter chi.Router) {
		apiRouter.Use(ratelimit.New(30, time.Minute, configuration.TrustedProxyCIDRs...).Middleware, authmw.Middleware(verifier))
		api.HandlerWithOptions(handler, api.ChiServerOptions{
			BaseRouter: apiRouter,
			ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
				apierror.Write(w, apierror.BadRequest("Некорректные параметры запроса"))
			},
		})
	})

	server := &http.Server{
		Addr:              configuration.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("gateway HTTP server started", "address", configuration.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-shutdownSignal.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), configuration.ShutdownTimeout)
		defer cancel()
		if err = server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown gateway: %w", err)
		}
		return nil
	case err = <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func defaultUnaryTimeout(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		request, response any,
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		if _, exists := ctx.Deadline(); exists {
			return invoker(ctx, method, request, response, connection, options...)
		}
		callContext, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return invoker(callContext, method, request, response, connection, options...)
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
		return fmt.Errorf("healthcheck returned %s", response.Status)
	}
	return nil
}
