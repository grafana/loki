package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/logentry/metric"
	"github.com/grafana/loki/pkg/promtail/api"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"

	"github.com/grafana/loki/pkg/helpers"
)

const (
	contentType  = "application/x-protobuf"
	maxErrMsgLen = 1024

	// Label reserved to override the tenant ID while processing
	// pipeline stages
	ReservedLabelTenantID = "__tenant_id__"

	LatencyLabel = "filename"
	HostLabel    = "host"
)

var (
	encodedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "encoded_bytes_total",
		Help:      "Number of bytes encoded and ready to send.",
	}, []string{HostLabel})
	sentBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_bytes_total",
		Help:      "Number of bytes sent.",
	}, []string{HostLabel})
	droppedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_bytes_total",
		Help:      "Number of bytes dropped because failed to be sent to the ingester after all retries.",
	}, []string{HostLabel})
	sentEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_entries_total",
		Help:      "Number of log entries sent to the ingester.",
	}, []string{HostLabel})
	droppedEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_entries_total",
		Help:      "Number of log entries dropped because failed to be sent to the ingester after all retries.",
	}, []string{HostLabel})
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "request_duration_seconds",
		Help:      "Duration of send requests.",
	}, []string{"status_code", HostLabel})
	batchRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "batch_retries_total",
		Help:      "Number of times batches has had to be retried.",
	}, []string{HostLabel})
	streamLag *metric.Gauges

	countersWithHost = []*prometheus.CounterVec{
		encodedBytes, sentBytes, droppedBytes, sentEntries, droppedEntries,
	}

	UserAgent = fmt.Sprintf("promtail/%s", version.Version)
)

func init() {
	prometheus.MustRegister(encodedBytes)
	prometheus.MustRegister(sentBytes)
	prometheus.MustRegister(droppedBytes)
	prometheus.MustRegister(sentEntries)
	prometheus.MustRegister(droppedEntries)
	prometheus.MustRegister(requestDuration)
	prometheus.MustRegister(batchRetries)
	var err error
	streamLag, err = metric.NewGauges("promtail_stream_lag_seconds",
		"Difference between current time and last batch timestamp for successful sends",
		metric.GaugeConfig{Action: "set"},
		int64(1*time.Minute.Seconds()), // This strips out files which update slowly and reduces noise in this metric.
	)
	if err != nil {
		panic(err)
	}
	prometheus.MustRegister(streamLag)
}

// Client pushes entries to Loki and can be stopped
type Client interface {
	api.EntryHandler
	// Stop goroutine sending batch of entries without retries.
	StopNow()
}

// Client for pushing logs in snappy-compressed protos over HTTP.
type client struct {
	logger  log.Logger
	cfg     Config
	client  *http.Client
	entries chan api.Entry

	once sync.Once
	wg   sync.WaitGroup

	externalLabels model.LabelSet

	// ctx is used in any upstream calls from the `client`.
	ctx    context.Context
	cancel context.CancelFunc
}

// New makes a new Client.
func New(cfg Config, logger log.Logger) (Client, error) {
	if cfg.URL.URL == nil {
		return nil, errors.New("client needs target URL")
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &client{
		logger:  log.With(logger, "component", "client", "host", cfg.URL.Host),
		cfg:     cfg,
		entries: make(chan api.Entry),

		externalLabels: cfg.ExternalLabels.LabelSet,
		ctx:            ctx,
		cancel:         cancel,
	}

	err := cfg.Client.Validate()
	if err != nil {
		return nil, err
	}

	c.client, err = config.NewClientFromConfig(cfg.Client, "promtail", false, false)
	if err != nil {
		return nil, err
	}

	c.client.Timeout = cfg.Timeout

	// Initialize counters to 0 so the metrics are exported before the first
	// occurrence of incrementing to avoid missing metrics.
	for _, counter := range countersWithHost {
		counter.WithLabelValues(c.cfg.URL.Host).Add(0)
	}

	c.wg.Add(1)
	go c.run()
	return c, nil
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
			batch, ok := batches[tenantID]

			// If the batch doesn't exist yet, we create a new one with the entry
			if !ok {
				batches[tenantID] = newBatch(e)
				break
			}

			// If adding the entry to the batch will increase the size over the max
			// size allowed, we do send the current batch and then create a new one
			if batch.sizeBytesAfter(e) > c.cfg.BatchSize {
				c.sendBatch(tenantID, batch)

				batches[tenantID] = newBatch(e)
				break
			}

			// The max size of the batch isn't reached, so we can add the entry
			batch.add(e)

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

func (c *client) sendBatch(tenantID string, batch *batch) {
	buf, entriesCount, err := batch.encode()
	if err != nil {
		level.Error(c.logger).Log("msg", "error encoding batch", "error", err)
		return
	}
	bufBytes := float64(len(buf))
	encodedBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)

	backoff := util.NewBackoff(c.ctx, c.cfg.BackoffConfig)
	var status int
	for {
		start := time.Now()
		// send uses `timeout` internally, so `context.Background` is good enough.
		status, err = c.send(context.Background(), tenantID, buf)

		requestDuration.WithLabelValues(strconv.Itoa(status), c.cfg.URL.Host).Observe(time.Since(start).Seconds())

		if err == nil {
			sentBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)
			sentEntries.WithLabelValues(c.cfg.URL.Host).Add(float64(entriesCount))
			for _, s := range batch.streams {
				lbls, err := parser.ParseMetric(s.Labels)
				if err != nil {
					// is this possible?
					level.Warn(c.logger).Log("msg", "error converting stream label string to label.Labels, cannot update lagging metric", "error", err)
					return
				}
				var lblSet model.LabelSet
				for i := range lbls {
					if lbls[i].Name == LatencyLabel {
						lblSet = model.LabelSet{
							model.LabelName(HostLabel):    model.LabelValue(c.cfg.URL.Host),
							model.LabelName(LatencyLabel): model.LabelValue(lbls[i].Value),
						}
					}
				}
				if lblSet != nil {
					streamLag.With(lblSet).Set(time.Since(s.Entries[len(s.Entries)-1].Timestamp).Seconds())
				}
			}
			return
		}

		// Only retry 429s, 500s and connection-level errors.
		if status > 0 && status != 429 && status/100 != 5 {
			break
		}

		level.Warn(c.logger).Log("msg", "error sending batch, will retry", "status", status, "error", err)
		batchRetries.WithLabelValues(c.cfg.URL.Host).Inc()
		backoff.Wait()

		// Make sure it sends at least once before checking for retry.
		if !backoff.Ongoing() {
			break
		}
	}

	if err != nil {
		level.Error(c.logger).Log("msg", "final error sending batch", "status", status, "error", err)
		droppedBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)
		droppedEntries.WithLabelValues(c.cfg.URL.Host).Add(float64(entriesCount))
	}
}

func (c *client) send(ctx context.Context, tenantID string, buf []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequest("POST", c.cfg.URL.String(), bytes.NewReader(buf))
	if err != nil {
		return -1, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", UserAgent)

	// If the tenant ID is not empty promtail is running in multi-tenant mode, so
	// we should send it to Loki
	if tenantID != "" {
		req.Header.Set("X-Scope-OrgID", tenantID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, err
	}
	defer helpers.LogError("closing response body", resp.Body.Close)

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
	delete(e.Labels, ReservedLabelTenantID)
	return e, tenantID
}

func (c *client) UnregisterLatencyMetric(labels model.LabelSet) {
	labels[HostLabel] = model.LabelValue(c.cfg.URL.Host)
	streamLag.Delete(labels)
}
