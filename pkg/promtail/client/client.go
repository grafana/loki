package client

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/promtail/api"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

const (
	contentType  = "application/x-protobuf"
	maxErrMsgLen = 1024

	// Label reserved to override the tenant ID while processing
	// pipeline stages
	ReservedLabelTenantID = "__tenant_id__"
)

var (
	encodedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "encoded_bytes_total",
		Help:      "Number of bytes encoded and ready to send.",
	}, []string{"host"})
	sentBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_bytes_total",
		Help:      "Number of bytes sent.",
	}, []string{"host"})
	droppedBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_bytes_total",
		Help:      "Number of bytes dropped because failed to be sent to the ingester after all retries.",
	}, []string{"host"})
	sentEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_entries_total",
		Help:      "Number of log entries sent to the ingester.",
	}, []string{"host"})
	droppedEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_entries_total",
		Help:      "Number of log entries dropped because failed to be sent to the ingester after all retries.",
	}, []string{"host"})
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "request_duration_seconds",
		Help:      "Duration of send requests.",
	}, []string{"status_code", "host"})
)

func init() {
	prometheus.MustRegister(encodedBytes)
	prometheus.MustRegister(sentBytes)
	prometheus.MustRegister(droppedBytes)
	prometheus.MustRegister(sentEntries)
	prometheus.MustRegister(droppedEntries)
	prometheus.MustRegister(requestDuration)
}

// Client pushes entries to Loki and can be stopped
type Client interface {
	api.EntryHandler
	// Stop goroutine sending batch of entries.
	Stop()
}

// Client for pushing logs in snappy-compressed protos over HTTP.
type client struct {
	logger  log.Logger
	cfg     Config
	client  *http.Client
	quit    chan struct{}
	once    sync.Once
	entries chan entry
	wg      sync.WaitGroup

	externalLabels model.LabelSet
}

type entry struct {
	tenantID string
	labels   model.LabelSet
	logproto.Entry
}

// New makes a new Client.
func New(cfg Config, logger log.Logger) (Client, error) {
	c := &client{
		logger:  log.With(logger, "component", "client", "host", cfg.URL.Host),
		cfg:     cfg,
		quit:    make(chan struct{}),
		entries: make(chan entry),

		externalLabels: cfg.ExternalLabels.LabelSet,
	}

	err := cfg.Client.Validate()
	if err != nil {
		return nil, err
	}

	c.client, err = config.NewClientFromConfig(cfg.Client, "promtail", false)
	if err != nil {
		return nil, err
	}

	c.client.Timeout = cfg.Timeout

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
		case <-c.quit:
			return

		case e := <-c.entries:
			batch, ok := batches[e.tenantID]

			// If the batch doesn't exist yet, we create a new one with the entry
			if !ok {
				batches[e.tenantID] = newBatch(e)
				break
			}

			// If adding the entry to the batch will increase the size over the max
			// size allowed, we do send the current batch and then create a new one
			if batch.sizeBytesAfter(e) > c.cfg.BatchSize {
				c.sendBatch(e.tenantID, batch)

				batches[e.tenantID] = newBatch(e)
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

func (c *client) sendBatch(tenantID string, batch *batch) {
	buf, entriesCount, err := batch.encode()
	if err != nil {
		level.Error(c.logger).Log("msg", "error encoding batch", "error", err)
		return
	}
	bufBytes := float64(len(buf))
	encodedBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)

	ctx := context.Background()
	backoff := util.NewBackoff(ctx, c.cfg.BackoffConfig)
	var status int
	for backoff.Ongoing() {
		start := time.Now()
		status, err = c.send(ctx, tenantID, buf)
		requestDuration.WithLabelValues(strconv.Itoa(status), c.cfg.URL.Host).Observe(time.Since(start).Seconds())

		if err == nil {
			sentBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)
			sentEntries.WithLabelValues(c.cfg.URL.Host).Add(float64(entriesCount))
			return
		}

		// Only retry 500s and connection-level errors.
		if status > 0 && status/100 != 5 {
			break
		}

		level.Warn(c.logger).Log("msg", "error sending batch, will retry", "status", status, "error", err)
		backoff.Wait()
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
	c.once.Do(func() { close(c.quit) })
	c.wg.Wait()
}

// Handle implement EntryHandler; adds a new line to the next batch; send is async.
func (c *client) Handle(ls model.LabelSet, t time.Time, s string) error {
	if len(c.externalLabels) > 0 {
		ls = c.externalLabels.Merge(ls)
	}

	// Get the tenant  ID in case it has been overridden while processing
	// the pipeline stages, then remove the special label
	tenantID := c.getTenantID(ls)
	if _, ok := ls[ReservedLabelTenantID]; ok {
		// Clone the label set to not manipulate the input one
		ls = ls.Clone()
		delete(ls, ReservedLabelTenantID)
	}

	c.entries <- entry{tenantID, ls, logproto.Entry{
		Timestamp: t,
		Line:      s,
	}}
	return nil
}
