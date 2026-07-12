package httpx

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultHTTPMetrics = newHTTPMetrics()
var defaultGauges = struct {
	sync.RWMutex
	values map[string]float64
}{values: make(map[string]float64)}

type metricKey struct {
	Method, Path string
	Status       int
}
type metricValue struct {
	Count    uint64
	Duration float64
	Buckets  [8]uint64
}
type httpMetrics struct {
	mu     sync.RWMutex
	values map[metricKey]*metricValue
}

var durationBuckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}

func newHTTPMetrics() *httpMetrics { return &httpMetrics{values: make(map[metricKey]*metricValue)} }

// Metrics records RED request metrics in Prometheus text format.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := newResponseWriter(w)
		next.ServeHTTP(writer, r)
		path := r.Pattern
		if path == "" {
			path = redactedPath(r.URL.Path)
		}
		defaultHTTPMetrics.observe(metricKey{Method: r.Method, Path: path, Status: writer.status}, time.Since(started).Seconds())
	})
}

// MetricsHandler exposes process-local HTTP RED metrics for Prometheus.
func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(defaultHTTPMetrics.render() + renderGauges()))
	})
}

// SetGauge publishes a process-local operational gauge. Labels must already be
// formatted as a stable Prometheus label set (for example `subject="name"`).
func SetGauge(name, labels string, value float64) {
	if name == "" {
		return
	}
	key := name
	if labels != "" {
		key += "{" + labels + "}"
	}
	defaultGauges.Lock()
	defaultGauges.values[key] = value
	defaultGauges.Unlock()
}

// AddGauge increments an operational gauge, useful for process-local DLQ depth signals.
func AddGauge(name, labels string, delta float64) {
	if name == "" {
		return
	}
	key := name
	if labels != "" {
		key += "{" + labels + "}"
	}
	defaultGauges.Lock()
	defaultGauges.values[key] += delta
	defaultGauges.Unlock()
}

func renderGauges() string {
	defaultGauges.RLock()
	defer defaultGauges.RUnlock()
	keys := make([]string, 0, len(defaultGauges.values))
	for key := range defaultGauges.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s %g\n", key, defaultGauges.values[key])
	}
	return b.String()
}

func (m *httpMetrics) observe(key metricKey, duration float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.values[key]
	if value == nil {
		value = &metricValue{}
		m.values[key] = value
	}
	value.Count++
	value.Duration += duration
	for i, upper := range durationBuckets {
		if duration <= upper {
			value.Buckets[i]++
		}
	}
}

func (m *httpMetrics) render() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]metricKey, 0, len(m.values))
	for key := range m.values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Path != keys[j].Path {
			return keys[i].Path < keys[j].Path
		}
		if keys[i].Method != keys[j].Method {
			return keys[i].Method < keys[j].Method
		}
		return keys[i].Status < keys[j].Status
	})
	var b strings.Builder
	b.WriteString("# HELP teamos_http_requests_total Общее число HTTP-запросов.\n# TYPE teamos_http_requests_total counter\n")
	for _, key := range keys {
		value := m.values[key]
		labels := metricLabels(key)
		fmt.Fprintf(&b, "teamos_http_requests_total{%s} %d\n", labels, value.Count)
	}
	b.WriteString("# HELP teamos_http_request_duration_seconds Длительность HTTP-запросов.\n# TYPE teamos_http_request_duration_seconds histogram\n")
	for _, key := range keys {
		value := m.values[key]
		labels := metricLabels(key)
		for i, upper := range durationBuckets {
			fmt.Fprintf(&b, "teamos_http_request_duration_seconds_bucket{%s,le=\"%s\"} %d\n", labels, strconv.FormatFloat(upper, 'f', -1, 64), value.Buckets[i])
		}
		fmt.Fprintf(&b, "teamos_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, value.Count)
		fmt.Fprintf(&b, "teamos_http_request_duration_seconds_sum{%s} %g\n", labels, value.Duration)
		fmt.Fprintf(&b, "teamos_http_request_duration_seconds_count{%s} %d\n", labels, value.Count)
	}
	return b.String()
}

func metricLabels(key metricKey) string {
	return fmt.Sprintf("method=\"%s\",path=\"%s\",status=\"%d\"", escapeLabel(key.Method), escapeLabel(key.Path), key.Status)
}
func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return strings.ReplaceAll(value, "\n", `\n`)
}
