package httpx

import (
	"context"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// SetupTelemetry configures OTLP trace export when OTEL_EXPORTER_OTLP_ENDPOINT is set.
// The returned function must be called during graceful shutdown.
func SetupTelemetry(serviceName string) (func() error, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")), "/")
	if endpoint == "" {
		return func() error { return nil }, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"))
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func() error {
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		return provider.Shutdown(shutdown)
	}, nil
}
