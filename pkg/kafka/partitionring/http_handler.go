package partitionring

import (
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
)

// ringHttpHandler wraps *ring.PartitionRingPageHandler and adds logging to incoming requests
type ringHttpHandler struct {
	handler *ring.PartitionRingPageHandler
	logger  log.Logger
}

// NewRingHttpHandler creates a new ringHttpHandler that wraps the given PartitionRingPageHandler
// with request logging capabilities
func NewRingHttpHandler(handler *ring.PartitionRingPageHandler, logger log.Logger) *ringHttpHandler {
	return &ringHttpHandler{
		handler: handler,
		logger:  logger,
	}
}

// ServeHTTP implements http.Handler interface, logs the incoming request and delegates to the wrapped handler
func (h *ringHttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Log the incoming request
	level.Info(h.logger).Log(
		"msg", "handling partition ring request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
		"query", r.URL.RawQuery,
	)

	// Create a response writer wrapper to capture status code
	wrapped := &responseWriterWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Delegate to the wrapped handler
	h.handler.ServeHTTP(wrapped, r)

	// Log the response
	duration := time.Since(start)
	level.Info(h.logger).Log(
		"msg", "partition ring request completed",
		"method", r.Method,
		"path", r.URL.Path,
		"status", wrapped.statusCode,
		"duration_ms", duration.Milliseconds(),
	)
}

// responseWriterWrapper wraps http.ResponseWriter to capture the status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before writing it
func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write implements http.ResponseWriter
func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	// If WriteHeader hasn't been called, Write implicitly calls WriteHeader(http.StatusOK)
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}
