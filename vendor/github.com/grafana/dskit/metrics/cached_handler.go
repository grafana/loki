package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/atomic"
	"golang.org/x/sync/singleflight"
)

const metricsResponseCacheSize = 8

// CachedHandlerFor is promhttp.HandlerFor with an in-memory response cache.
// A non-positive cacheDuration disables caching. Successful GET responses are
// cached for at most cacheDuration from the start of collection, independently
// for up to eight Accept/Accept-Encoding combinations. Requests with query
// parameters bypass the cache. Cached metrics can lag behind live values by
// cacheDuration; choose a duration appropriate for scrape intervals and alerts.
// With caching enabled, overlapping collections are always coalesced across
// variants and bypassed requests, regardless of opts.CoalesceGather.
func CachedHandlerFor(reg prometheus.Gatherer, opts promhttp.HandlerOpts, cacheDuration time.Duration) http.Handler {
	if cacheDuration <= 0 {
		return promhttp.HandlerFor(reg, opts)
	}

	timeout := opts.Timeout
	// Cache hits are written directly and refreshes run detached from requests, so
	// the timeout only applies to waiting for a refresh.
	opts.Timeout = 0
	var inFlight chan struct{}
	if opts.MaxRequestsInFlight > 0 {
		inFlight = make(chan struct{}, opts.MaxRequestsInFlight)
		opts.MaxRequestsInFlight = 0
	}
	reg = &metricsCoalescingGatherer{reg: reg}
	opts.CoalesceGather = false
	responses, err := lru.New[metricsResponseCacheKey, *metricsResponse](metricsResponseCacheSize)
	if err != nil {
		panic(err)
	}
	// Refreshes need separate error logs, but concurrency controls must span
	// every representation and the uncached path.
	bypass := limitMetricsRequests(promhttp.HandlerFor(reg, opts), inFlight)
	if timeout > 0 {
		bypass = http.TimeoutHandler(bypass, timeout, metricsTimeoutBody(timeout))
	}
	return &cachedMetricsHandler{
		reg:       reg,
		opts:      opts,
		inFlight:  inFlight,
		bypass:    bypass,
		duration:  cacheDuration,
		timeout:   timeout,
		now:       time.Now,
		responses: responses,
		refreshes: map[metricsResponseCacheKey]*metricsRefresh{},
	}
}

// metricsTimeoutBody matches the body promhttp writes when its own timeout expires.
func metricsTimeoutBody(timeout time.Duration) string {
	return fmt.Sprintf("Exceeded configured timeout of %v.\n", timeout)
}

type metricsResponseCacheKey struct{ accept, encoding string }

type metricsResponse struct {
	header  http.Header
	body    []byte
	expires time.Time
}

func (r *metricsResponse) writeTo(w http.ResponseWriter, code int) {
	for key, values := range r.header {
		// Preserve negotiation dimensions already added by outer middleware.
		if key == "Vary" {
			for _, value := range values {
				w.Header().Add(key, value)
			}
			continue
		}
		w.Header()[key] = slices.Clone(values)
	}
	w.WriteHeader(code)
	_, _ = w.Write(r.body)
}

// metricsRefresh is a collection shared by every request for the same key that
// arrives while it runs.
type metricsRefresh struct {
	done     chan struct{}
	response *metricsResponse
	code     int
	panicked any
}

type cachedMetricsHandler struct {
	reg      prometheus.Gatherer
	opts     promhttp.HandlerOpts
	inFlight chan struct{}
	bypass   http.Handler
	duration time.Duration
	timeout  time.Duration
	now      func() time.Time

	mu        sync.Mutex
	responses *lru.Cache[metricsResponseCacheKey, *metricsResponse]
	refreshes map[metricsResponseCacheKey]*metricsRefresh
}

func (h *cachedMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Filtered scrapes must not populate the full-registry snapshot, and arbitrary
	// query strings must not create an unbounded number of cache entries.
	if r.Method != http.MethodGet || (r.URL != nil && r.URL.RawQuery != "") {
		h.bypass.ServeHTTP(w, r)
		return
	}
	key := metricsResponseCacheKey{accept: r.Header.Get("Accept"), encoding: strings.Join(r.Header.Values("Accept-Encoding"), "\x00")}
	h.mu.Lock()
	if response, ok := h.responses.Get(key); ok && h.now().Before(response.expires) {
		h.mu.Unlock()
		response.writeTo(w, http.StatusOK)
		return
	}
	refresh, ok := h.refreshes[key]
	if !ok {
		refresh = &metricsRefresh{done: make(chan struct{})}
		h.refreshes[key] = refresh
		// The refresh outlives a canceled or timed-out first request so that the
		// other waiters still get its result.
		go h.refresh(r.Clone(context.WithoutCancel(r.Context())), key, refresh)
	}
	h.mu.Unlock()

	var timedOut <-chan time.Time
	if h.timeout > 0 {
		timer := time.NewTimer(h.timeout)
		defer timer.Stop()
		timedOut = timer.C
	}
	select {
	case <-refresh.done:
	case <-r.Context().Done():
		return
	case <-timedOut:
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, metricsTimeoutBody(h.timeout))
		return
	}
	if refresh.panicked != nil {
		panic(refresh.panicked)
	}
	refresh.response.writeTo(w, refresh.code)
}

func (h *cachedMetricsHandler) refresh(r *http.Request, key metricsResponseCacheKey, refresh *metricsRefresh) {
	cacheable := false
	defer func() {
		// Re-raised in waiters so PanicOnError behaves as it does without the cache.
		refresh.panicked = recover()
		h.mu.Lock()
		if cacheable {
			h.responses.Add(key, refresh.response)
		}
		delete(h.refreshes, key)
		h.mu.Unlock()
		close(refresh.done)
	}()

	// promhttp can return a partial body with status 200 on collection or encoding
	// errors. A per-refresh error log attributes those errors to this response only.
	errorLog := &metricsHandlerErrorLog{next: h.opts.ErrorLog}
	opts := h.opts
	opts.ErrorLog = errorLog
	start := h.now()
	writer := &metricsResponseWriter{header: make(http.Header)}
	limitMetricsRequests(promhttp.HandlerFor(h.reg, opts), h.inFlight).ServeHTTP(writer, r)
	if writer.code == 0 {
		writer.code = http.StatusOK
	}
	refresh.code = writer.code
	refresh.response = &metricsResponse{header: writer.header, body: writer.body.Bytes(), expires: start.Add(h.duration)}
	// With ContinueOnError, a partial response is what every uncached scrape would
	// get, so caching it avoids repeating a failing collection on each scrape.
	errorsAccepted := opts.ErrorHandling == promhttp.ContinueOnError || errorLog.errors.Load() == 0
	if writer.code == http.StatusOK && errorsAccepted && h.now().Before(refresh.response.expires) {
		// Buffer growth can leave nearly twice the payload size retained per variant.
		refresh.response.body = bytes.Clone(refresh.response.body)
		cacheable = true
	}
}

func limitMetricsRequests(next http.Handler, inFlight chan struct{}) http.Handler {
	if inFlight == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case inFlight <- struct{}{}:
			defer func() { <-inFlight }()
			next.ServeHTTP(w, r)
		default:
			http.Error(w, fmt.Sprintf("Limit of concurrent requests reached (%d), try again later.", cap(inFlight)), http.StatusServiceUnavailable)
		}
	})
}

type metricsCoalescingGatherer struct {
	reg   prometheus.Gatherer
	group singleflight.Group
}

type metricsGatherResult struct {
	families []*dto.MetricFamily
	err      error
	panicked any
}

func (g *metricsCoalescingGatherer) Gather() ([]*dto.MetricFamily, error) {
	v, _, _ := g.group.Do("", func() (any, error) {
		result := &metricsGatherResult{}
		func() {
			// Keep the original panic value so each handler applies normal recovery.
			defer func() { result.panicked = recover() }()
			result.families, result.err = g.reg.Gather()
		}()
		return result, nil
	})
	result := v.(*metricsGatherResult)
	if result.panicked != nil {
		panic(result.panicked)
	}
	return slices.Clone(result.families), result.err
}

type metricsResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (w *metricsResponseWriter) Header() http.Header { return w.header }
func (w *metricsResponseWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *metricsResponseWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(p)
}

type metricsHandlerErrorLog struct {
	next   promhttp.Logger
	errors atomic.Uint64
}

func (l *metricsHandlerErrorLog) Println(v ...any) {
	l.errors.Inc()
	if l.next != nil {
		l.next.Println(v...)
	}
}
