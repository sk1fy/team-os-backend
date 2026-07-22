package httpx

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

const RequestIDHeader = "X-Request-ID"

type contextKey uint8

const (
	requestIDKey contextKey = iota
	logAttributesKey
	logAttributeCollectorKey
)

// Middleware follows the net/http middleware convention.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in the listed order: the first item is outermost.
func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for index := len(middleware) - 1; index >= 0; index-- {
		handler = middleware[index](handler)
	}
	return handler
}

// RequestID accepts a bounded caller-provided identifier or generates a UUID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if requestID == "" || len(requestID) > 128 {
			requestID = newRequestID()
		}

		w.Header().Set(RequestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request identifier installed by RequestID.
func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}

// WithLogAttributes adds domain-aware structured fields such as company_id and
// user_id without coupling this package to authentication claims.
func WithLogAttributes(ctx context.Context, attributes ...slog.Attr) context.Context {
	if collector, ok := ctx.Value(logAttributeCollectorKey).(*[]slog.Attr); ok && collector != nil {
		*collector = append(*collector, attributes...)
		return ctx
	}
	existing, _ := ctx.Value(logAttributesKey).([]slog.Attr)
	combined := make([]slog.Attr, 0, len(existing)+len(attributes))
	combined = append(combined, existing...)
	combined = append(combined, attributes...)
	return context.WithValue(ctx, logAttributesKey, combined)
}

// Logging emits one structured completion record per request.
func Logging(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			initial, _ := r.Context().Value(logAttributesKey).([]slog.Attr)
			collected := append([]slog.Attr(nil), initial...)
			r = r.WithContext(context.WithValue(r.Context(), logAttributeCollectorKey, &collected))
			writer := newResponseWriter(w)
			next.ServeHTTP(writer, r)

			attributes := []slog.Attr{
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", redactedPath(r.URL.Path)),
				slog.Int("status", writer.status),
				slog.Int64("response_bytes", writer.bytes),
				slog.Duration("duration", time.Since(startedAt)),
			}
			if spanContext := trace.SpanContextFromContext(r.Context()); spanContext.IsValid() {
				attributes = append(attributes, slog.String("trace_id", spanContext.TraceID().String()))
			}
			attributes = append(attributes, collected...)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "HTTP request completed", attributes...)
		})
	}
}

func redactedPath(path string) string {
	const invitePrefix = "/api/v1/auth/invites/"
	const accessLinkPrefix = "/api/v1/auth/access-link/"
	const academyAccessPrefix = "/api/v1/public/academy/access/"
	if strings.HasPrefix(path, accessLinkPrefix) {
		return accessLinkPrefix + ":token"
	}
	if strings.HasPrefix(path, academyAccessPrefix) {
		return redactFirstPathSegment(path, academyAccessPrefix, ":token")
	}
	if !strings.HasPrefix(path, invitePrefix) {
		return path
	}
	remainder := strings.TrimPrefix(path, invitePrefix)
	if strings.HasSuffix(remainder, "/accept") {
		return invitePrefix + ":token/accept"
	}
	return invitePrefix + ":token"
}

func redactFirstPathSegment(path, prefix, placeholder string) string {
	remainder := strings.TrimPrefix(path, prefix)
	if slash := strings.IndexByte(remainder, '/'); slash >= 0 {
		return prefix + placeholder + remainder[slash:]
	}
	return prefix + placeholder
}

// Recoverer converts a panic raised before response commit to a safe API error
// and always records the stack trace in server logs.
func Recoverer(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writer := newResponseWriter(w)
			defer func() {
				panicValue := recover()
				if panicValue == nil {
					return
				}

				logger.ErrorContext(
					r.Context(),
					"HTTP handler panic",
					"request_id", RequestIDFromContext(r.Context()),
					"panic", panicValue,
					"stack", string(debug.Stack()),
				)
				if !writer.wroteHeader {
					apierror.Write(writer, apierror.Internal())
				}
			}()

			next.ServeHTTP(writer, r)
		})
	}
}

// BodyLimit guards endpoints that do their own decoding. DecodeJSON applies the
// same limit directly and is preferable for JSON handlers.
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders constrains browser rendering of API responses and mirrors
// the server-side video provider allowlist.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-src https://www.youtube.com https://www.youtube-nocookie.com https://player.vimeo.com https://rutube.ru; media-src https:; object-src 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// Tracing instruments HTTP requests with the configured global OTel provider.
func Tracing(serviceName string) Middleware {
	return func(next http.Handler) http.Handler {
		// otelhttp derives URL, client address and user-agent attributes before it
		// invokes the next handler. Give instrumentation a privacy-safe clone, then
		// restore the original request together with the newly created span context
		// for routing, authentication and rate limiting.
		instrumented := otelhttp.NewMiddleware(serviceName)(http.HandlerFunc(func(w http.ResponseWriter, traced *http.Request) {
			original, ok := traced.Context().Value(originalTelemetryRequestKey{}).(*http.Request)
			if !ok || original == nil {
				next.ServeHTTP(w, traced)
				return
			}
			next.ServeHTTP(w, original.WithContext(traced.Context()))
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), originalTelemetryRequestKey{}, r)
			safe := r.Clone(ctx)
			safeURL := *r.URL
			safeURL.Path = redactedPath(r.URL.Path)
			safeURL.RawPath = ""
			safeURL.RawQuery = ""
			safeURL.ForceQuery = false
			safe.URL = &safeURL
			safe.RequestURI = safeURL.RequestURI()
			safe.RemoteAddr = ""
			safe.Header = r.Header.Clone()
			for _, header := range []string{
				"Authorization", "Cookie", "Forwarded", "Referer", "User-Agent",
				"X-Forwarded-For", "X-Real-IP",
			} {
				safe.Header.Del(header)
			}
			instrumented.ServeHTTP(w, safe)
		})
	}
}

type originalTelemetryRequestKey struct{}

func newRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		// The fallback remains unique enough for correlation and avoids making a
		// request fail solely because the kernel CSPRNG is unavailable.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func newResponseWriter(writer http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: writer, status: http.StatusOK}
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *responseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		written, err := readerFrom.ReadFrom(reader)
		w.bytes += written
		return written, err
	}
	written, err := io.Copy(w.ResponseWriter, reader)
	w.bytes += written
	return written, err
}

func (w *responseWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return errors.ErrUnsupported
	}
	return pusher.Push(target, options)
}
