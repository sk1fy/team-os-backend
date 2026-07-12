package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsRecordsRateStatusAndDuration(t *testing.T) {
	previous := defaultHTTPMetrics
	defaultHTTPMetrics = newHTTPMetrics()
	defer func() { defaultHTTPMetrics = previous }()
	handler := Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Pattern = "GET /things/{id}"
		w.WriteHeader(http.StatusCreated)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/things/secret-id", nil))
	output := defaultHTTPMetrics.render()
	if !strings.Contains(output, `teamos_http_requests_total{method="GET",path="GET /things/{id}",status="201"} 1`) {
		t.Fatalf("unexpected metrics:\n%s", output)
	}
	if strings.Contains(output, "secret-id") {
		t.Fatal("metrics must not expose path parameters")
	}
}
