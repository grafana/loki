package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	lokiutil "github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/build"
)

const (
	contentType  = "application/x-protobuf"
	maxErrMsgLen = 1024

	// Label reserved to override the tenant ID while processing
	// pipeline stages
	ReservedLabelTenantID = "__tenant_id__"

	LatencyLabel = "filename"
	HostLabel    = "host"
	ClientLabel  = "client"
	TenantLabel  = "tenant"
	ReasonLabel  = "reason"

	ReasonGeneric       = "ingester_error"
	ReasonRateLimited   = "rate_limited"
	ReasonStreamLimited = "stream_limited"
	ReasonLineTooLong   = "line_too_long"
)

var Reasons = []string{ReasonGeneric, ReasonRateLimited, ReasonStreamLimited, ReasonLineTooLong}

var UserAgent = fmt.Sprintf("promtail/%s", build.Version)

type Metrics struct {
	encodedBytes                 *prometheus.CounterVec
	sentBytes                    *prometheus.CounterVec
	droppedBytes                 *prometheus.CounterVec
	sentEntries                  *prometheus.CounterVec
	droppedEntries               *prometheus.CounterVec
	mutatedEntries               *prometheus.CounterVec
	mutatedBytes                 *prometheus.CounterVec
	requestDuration              *prometheus.HistogramVec
	batchRetries                 *prometheus.CounterVec
	countersWithHost             []*prometheus.CounterVec
	countersWithHostTenant       []*prometheus.CounterVec
	countersWithHostTenantReason []*prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics

	m.encodedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "encoded_bytes_total",
		Help:      "Number of bytes encoded and ready to send.",
	}, []string{HostLabel})
	m.sentBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_bytes_total",
		Help:      "Number of bytes sent.",
	}, []string{HostLabel})
	m.droppedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_bytes_total",
		Help:      "Number of bytes dropped because failed to be sent to the ingester after all retries.",
	}, []string{HostLabel, TenantLabel, ReasonLabel})
	m.sentEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_entries_total",
		Help:      "Number of log entries sent to the ingester.",
	}, []string{HostLabel})
	m.droppedEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_entries_total",
		Help:      "Number of log entries dropped because failed to be sent to the ingester after all retries.",
	}, []string{HostLabel, TenantLabel, ReasonLabel})
	m.mutatedEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "mutated_entries_total",
		Help:      "The total number of log entries that have been mutated.",
	}, []string{HostLabel, TenantLabel, ReasonLabel})
	m.mutatedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "mutated_bytes_total",
		Help:      "The total number of bytes that have been mutated.",
	}, []string{HostLabel, TenantLabel, ReasonLabel})
	m.requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "request_duration_seconds",
		Help:      "Duration of send requests.",
	}, []string{"status_code", HostLabel})
	m.batchRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "batch_retries_total",
		Help:      "Number of times batches has had to be retried.",
	}, []string{HostLabel, TenantLabel})

	m.countersWithHost = []*prometheus.CounterVec{
		m.encodedBytes, m.sentBytes, m.sentEntries,
	}

	m.countersWithHostTenant = []*prometheus.CounterVec{
		m.batchRetries,
	}

	m.countersWithHostTenantReason = []*prometheus.CounterVec{
		m.droppedBytes, m.droppedEntries, m.mutatedEntries, m.mutatedBytes,
	}

	if reg != nil {
		m.encodedBytes = mustRegisterOrGet(reg, m.encodedBytes).(*prometheus.CounterVec)
		m.sentBytes = mustRegisterOrGet(reg, m.sentBytes).(*prometheus.CounterVec)
		m.droppedBytes = mustRegisterOrGet(reg, m.droppedBytes).(*prometheus.CounterVec)
		m.sentEntries = mustRegisterOrGet(reg, m.sentEntries).(*prometheus.CounterVec)
		m.droppedEntries = mustRegisterOrGet(reg, m.droppedEntries).(*prometheus.CounterVec)
		m.mutatedEntries = mustRegisterOrGet(reg, m.mutatedEntries).(*prometheus.CounterVec)
		m.mutatedBytes = mustRegisterOrGet(reg, m.mutatedBytes).(*prometheus.CounterVec)
		m.requestDuration = mustRegisterOrGet(reg, m.requestDuration).(*prometheus.HistogramVec)
		m.batchRetries = mustRegisterOrGet(reg, m.batchRetries).(*prometheus.CounterVec)
	}

	return &m
}

func mustRegisterOrGet(reg prometheus.Registerer, c prometheus.Collector) prometheus.Collector {
	if err := reg.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return are.ExistingCollector
		}
		panic(err)
	}
	return c
}

// Client pushes entries to Loki and can be stopped
type Client interface {
	api.EntryHandler
	// Stop goroutine sending batch of entries without retries.
	StopNow()
	Name() string
}

// Client for pushing logs in snappy-compressed protos over HTTP.
type client struct {
	name    string
	metrics *Metrics
	logger  log.Logger
	cfg     Config
	client  *http.Client
	entries chan api.Entry

	once sync.Once
	wg   sync.WaitGroup

	externalLabels model.LabelSet

	// ctx is used in any upstream calls from the `client`.
	ctx                 context.Context
	cancel              context.CancelFunc
	maxStreams          int
	maxLineSize         int
	maxLineSizeTruncate bool
}

// Tripperware can wrap a roundtripper.
type Tripperware func(http.RoundTripper) http.RoundTripper

// New makes a new Client.
func New(metrics *Metrics, cfg Config, maxStreams, maxLineSize int, maxLineSizeTruncate bool, logger log.Logger) (Client, error) {
	return newClient(metrics, cfg, maxStreams, maxLineSize, maxLineSizeTruncate, logger)
}

func newClient(metrics *Metrics, cfg Config, maxStreams, maxLineSize int, maxLineSizeTruncate bool, logger log.Logger) (*client, error) {

	if cfg.URL.URL == nil {
		return nil, errors.New("client needs target URL")
	}
	if metrics == nil {
		return nil, errors.New("metrics must be instantiated")
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &client{
		logger:  log.With(logger, "component", "client", "host", cfg.URL.Host),
		cfg:     cfg,
		entries: make(chan api.Entry),
		metrics: metrics,
		name:    asSha256(cfg),

		externalLabels:      cfg.ExternalLabels.LabelSet,
		ctx:                 ctx,
		cancel:              cancel,
		maxStreams:          maxStreams,
		maxLineSize:         maxLineSize,
		maxLineSizeTruncate: maxLineSizeTruncate,
	}
	if cfg.Name != "" {
		c.name = cfg.Name
	}

	err := cfg.Client.Validate()
	if err != nil {
		return nil, err
	}

	c.client, err = config.NewClientFromConfig(cfg.Client, "promtail", config.WithHTTP2Disabled())
	if err != nil {
		return nil, err
	}

	c.client.Timeout = cfg.Timeout

	// Initialize counters to 0 so the metrics are exported before the first
	// occurrence of incrementing to avoid missing metrics.
	for _, counter := range c.metrics.countersWithHost {
		counter.WithLabelValues(c.cfg.URL.Host).Add(0)
	}

	c.wg.Add(1)
	go c.run()
	return c, nil
}

// NewWithTripperware creates a new Loki client with a custom tripperware.
func NewWithTripperware(metrics *Metrics, cfg Config, maxStreams, maxLineSize int, maxLineSizeTruncate bool, logger log.Logger, tp Tripperware) (Client, error) {
	c, err := newClient(metrics, cfg, maxStreams, maxLineSize, maxLineSizeTruncate, logger)
	if err != nil {
		return nil, err
	}

	if tp != nil {
		c.client.Transport = tp(c.client.Transport)
	}

	return c, nil
}

func (c *client) initBatchMetrics(tenantID string) {
	// Initialize counters to 0 so the metrics are exported before the first
	// occurrence of incrementing to avoid missing metrics.
	for _, counter := range c.metrics.countersWithHostTenantReason {
		for _, reason := range Reasons {
			counter.WithLabelValues(c.cfg.URL.Host, tenantID, reason).Add(0)
		}
	}

	for _, counter := range c.metrics.countersWithHostTenant {
		counter.WithLabelValues(c.cfg.URL.Host, tenantID).Add(0)
	}
}

func (c *client) run() {
	batches := map[string]*batch{}

	// Given the client handles multiple batches (1 per tenant) and each batch
	// can be created at a different point in time, we look for batches whose
	// max wait time has been reached every 10 times per BatchWait, so that the
	// maximum delay we have sending batches is 10% of the max waiting time.
	// We apply a cap of 10ms to the ticker, to avoid too frequent checks in
	// case the BatchWait is very low.
	minWaitCheckFrequency := 10 * time.Millisecond
	maxWaitCheckFrequency := c.cfg.BatchWait / 10
	if maxWaitCheckFrequency < minWaitCheckFrequency {
		maxWaitCheckFrequency = minWaitCheckFrequency
	}

	maxWaitCheck := time.NewTicker(maxWaitCheckFrequency)

	defer func() {
		maxWaitCheck.Stop()
		// Send all pending batches
		for tenantID, batch := range batches {
			c.sendBatch(tenantID, batch)
		}

		c.wg.Done()
	}()

	for {
		select {
		case e, ok := <-c.entries:
			if !ok {
				return
			}

			e, tenantID := c.processEntry(e)

			// Either drop or mutate the log entry because its length is greater than maxLineSize. maxLineSize == 0 means disabled.
			if c.maxLineSize != 0 && len(e.Line) > c.maxLineSize {
				if !c.maxLineSizeTruncate {
					c.metrics.droppedEntries.WithLabelValues(c.cfg.URL.Host, tenantID, ReasonLineTooLong).Inc()
					c.metrics.droppedBytes.WithLabelValues(c.cfg.URL.Host, tenantID, ReasonLineTooLong).Add(float64(len(e.Line)))
					break
				}

				c.metrics.mutatedEntries.WithLabelValues(c.cfg.URL.Host, tenantID, ReasonLineTooLong).Inc()
				c.metrics.mutatedBytes.WithLabelValues(c.cfg.URL.Host, tenantID, ReasonLineTooLong).Add(float64(len(e.Line) - c.maxLineSize))
				e.Line = e.Line[:c.maxLineSize]
			}

			batch, ok := batches[tenantID]

			// If the batch doesn't exist yet, we create a new one with the entry
			if !ok {
				batches[tenantID] = newBatch(c.maxStreams, e)
				c.initBatchMetrics(tenantID)
				break
			}

			// If adding the entry to the batch will increase the size over the max
			// size allowed, we do send the current batch and then create a new one
			if batch.sizeBytesAfter(e) > c.cfg.BatchSize {
				c.sendBatch(tenantID, batch)

				batches[tenantID] = newBatch(c.maxStreams, e)
				break
			}

			// The max size of the batch isn't reached, so we can add the entry
			err := batch.add(e)
			if err != nil {
				level.Error(c.logger).Log("msg", "batch add err", "tenant", tenantID, "error", err)
				reason := ReasonGeneric
				if err.Error() == errMaxStreamsLimitExceeded {
					reason = ReasonStreamLimited
				}
				c.metrics.droppedBytes.WithLabelValues(c.cfg.URL.Host, tenantID, reason).Add(float64(len(e.Line)))
				c.metrics.droppedEntries.WithLabelValues(c.cfg.URL.Host, tenantID, reason).Inc()
				return
			}
		case <-maxWaitCheck.C:
			// Send all batches whose max wait time has been reached
			for tenantID, batch := range batches {
				if batch.age() < c.cfg.BatchWait {
					continue
				}

				c.sendBatch(tenantID, batch)
				delete(batches, tenantID)
			}
		}
	}
}

func (c *client) Chan() chan<- api.Entry {
	return c.entries
}

func asSha256(o interface{}) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%v", o)))

	temp := fmt.Sprintf("%x", h.Sum(nil))
	return temp[:6]
}

func batchIsRateLimited(status int) bool {
	return status == 429
}

func (c *client) sendBatch(tenantID string, batch *batch) {
	buf, entriesCount, err := batch.encode()
	if err != nil {
		level.Error(c.logger).Log("msg", "error encoding batch", "error", err)
		return
	}
	bufBytes := float64(len(buf))
	c.metrics.encodedBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)

	backoff := backoff.New(c.ctx, c.cfg.BackoffConfig)
	var status int
	for {
		start := time.Now()
		// send uses `timeout` internally, so `context.Background` is good enough.
		status, err = c.send(context.Background(), tenantID, buf)

		c.metrics.requestDuration.WithLabelValues(strconv.Itoa(status), c.cfg.URL.Host).Observe(time.Since(start).Seconds())

		// Immediately drop rate limited batches to avoid HOL blocking for other tenants not experiencing throttling
		if c.cfg.DropRateLimitedBatches && batchIsRateLimited(status) {
			level.Warn(c.logger).Log("msg", "dropping batch due to rate limiting applied at ingester")
			c.metrics.droppedBytes.WithLabelValues(c.cfg.URL.Host, tenantID, ReasonRateLimited).Add(bufBytes)
			c.metrics.droppedEntries.WithLabelValues(c.cfg.URL.Host, tenantID, ReasonRateLimited).Add(float64(entriesCount))
			return
		}

		if err == nil {
			c.metrics.sentBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)
			c.metrics.sentEntries.WithLabelValues(c.cfg.URL.Host).Add(float64(entriesCount))

			return
		}

		// Only retry 429s, 500s and connection-level errors.
		if status > 0 && !batchIsRateLimited(status) && status/100 != 5 {
			break
		}

		level.Warn(c.logger).Log("msg", "error sending batch, will retry", "status", status, "tenant", tenantID, "error", err)
		c.metrics.batchRetries.WithLabelValues(c.cfg.URL.Host, tenantID).Inc()
		backoff.Wait()

		// Make sure it sends at least once before checking for retry.
		if !backoff.Ongoing() {
			break
		}
	}

	if err != nil {
		level.Error(c.logger).Log("msg", "final error sending batch", "status", status, "tenant", tenantID, "error", err)
		// If the reason for the last retry error was rate limiting, count the drops as such, even if the previous errors
		// were for a different reason
		dropReason := ReasonGeneric
		if batchIsRateLimited(status) {
			dropReason = ReasonRateLimited
		}
		c.metrics.droppedBytes.WithLabelValues(c.cfg.URL.Host, tenantID, dropReason).Add(bufBytes)
		c.metrics.droppedEntries.WithLabelValues(c.cfg.URL.Host, tenantID, dropReason).Add(float64(entriesCount))
	}
}

func (c *client) send(ctx context.Context, tenantID string, buf []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.URL.String(), bytes.NewReader(buf))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", UserAgent)

	// If the tenant ID is not empty promtail is running in multi-tenant mode, so
	// we should send it to Loki
	if tenantID != "" {
		req.Header.Set("X-Scope-OrgID", tenantID)
	}

	// Add custom headers on request
	if len(c.cfg.Headers) > 0 {
		for k, v := range c.cfg.Headers {
			if req.Header.Get(k) == "" {
				req.Header.Add(k, v)
			} else {
				level.Warn(c.logger).Log("msg", "custom header key already exists, skipping", "key", k)
			}
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, err
	}
	defer lokiutil.LogError("closing response body", resp.Body.Close)

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
	}
	return resp.StatusCode, err
}

func (c *client) getTenantID(labels model.LabelSet) string {
	// Check if it has been overridden while processing the pipeline stages
	if value, ok := labels[ReservedLabelTenantID]; ok {
		return string(value)
	}

	// Check if has been specified in the config
	if c.cfg.TenantID != "" {
		return c.cfg.TenantID
	}

	// Defaults to an empty string, which means the X-Scope-OrgID header
	// will not be sent
	return ""
}

// Stop the client.
func (c *client) Stop() {
	c.once.Do(func() { close(c.entries) })
	c.wg.Wait()
}

// StopNow stops the client without retries
func (c *client) StopNow() {
	// cancel will stop retrying http requests.
	c.cancel()
	c.Stop()
}

func (c *client) processEntry(e api.Entry) (api.Entry, string) {
	if len(c.externalLabels) > 0 {
		e.Labels = c.externalLabels.Merge(e.Labels)
	}
	tenantID := c.getTenantID(e.Labels)
	return e, tenantID
}

func (c *client) Name() string {
	return c.name
}
