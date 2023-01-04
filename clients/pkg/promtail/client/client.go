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
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/util"
	lokiutil "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/wal"
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

	TenantLabel = "tenant"
)

var UserAgent = fmt.Sprintf("promtail/%s", build.Version)

type Metrics struct {
	registerer         prometheus.Registerer
	encodedBytes       *prometheus.CounterVec
	sentBytes          *prometheus.CounterVec
	droppedBytes       *prometheus.CounterVec
	sentEntries        *prometheus.CounterVec
	droppedEntries     *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	batchRetries       *prometheus.CounterVec
	countersWithHost   []*prometheus.CounterVec
	countersWithTenant []*prometheus.CounterVec
	streamLag          *prometheus.GaugeVec
}

func NewMetrics(reg prometheus.Registerer, streamLagLabels []string) *Metrics {
	m := Metrics{
		registerer: reg,
	}

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
	}, []string{HostLabel, TenantLabel})
	m.sentEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_entries_total",
		Help:      "Number of log entries sent to the ingester.",
	}, []string{HostLabel})
	m.droppedEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "dropped_entries_total",
		Help:      "Number of log entries dropped because failed to be sent to the ingester after all retries.",
	}, []string{HostLabel, TenantLabel})
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

	m.countersWithTenant = []*prometheus.CounterVec{
		m.droppedBytes, m.droppedEntries, m.batchRetries,
	}

	streamLagLabelsMerged := []string{HostLabel, ClientLabel}
	streamLagLabelsMerged = append(streamLagLabelsMerged, streamLagLabels...)
	m.streamLag = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "stream_lag_seconds",
		Help:      "Difference between current time and last batch timestamp for successful sends",
	}, streamLagLabelsMerged)

	if reg != nil {
		m.encodedBytes = mustRegisterOrGet(reg, m.encodedBytes).(*prometheus.CounterVec)
		m.sentBytes = mustRegisterOrGet(reg, m.sentBytes).(*prometheus.CounterVec)
		m.droppedBytes = mustRegisterOrGet(reg, m.droppedBytes).(*prometheus.CounterVec)
		m.sentEntries = mustRegisterOrGet(reg, m.sentEntries).(*prometheus.CounterVec)
		m.droppedEntries = mustRegisterOrGet(reg, m.droppedEntries).(*prometheus.CounterVec)
		m.requestDuration = mustRegisterOrGet(reg, m.requestDuration).(*prometheus.HistogramVec)
		m.batchRetries = mustRegisterOrGet(reg, m.batchRetries).(*prometheus.CounterVec)
		m.streamLag = mustRegisterOrGet(reg, m.streamLag).(*prometheus.GaugeVec)
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
	name            string
	metrics         *Metrics
	streamLagLabels []string
	logger          log.Logger
	cfg             Config
	client          *http.Client
	entries         chan api.Entry

	once sync.Once
	wg   sync.WaitGroup

	externalLabels model.LabelSet

	// ctx is used in any upstream calls from the `client`.
	ctx        context.Context
	cancel     context.CancelFunc
	maxStreams int

	wal clientWAL
}

// Tripperware can wrap a roundtripper.
type Tripperware func(http.RoundTripper) http.RoundTripper

// New makes a new Client.
func New(metrics *Metrics, cfg Config, streamLagLabels []string, maxStreams int, logger log.Logger) (Client, error) {
	if cfg.StreamLagLabels.String() != "" {
		return nil, fmt.Errorf("client config stream_lag_labels is deprecated in favour of the config file options block field, and will be ignored: %+v", cfg.StreamLagLabels.String())
	}
	return newClient(metrics, cfg, streamLagLabels, maxStreams, logger)
}

func newClient(metrics *Metrics, cfg Config, streamLagLabels []string, maxStreams int, logger log.Logger) (*client, error) {

	if cfg.URL.URL == nil {
		return nil, errors.New("client needs target URL")
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &client{
		logger:          log.With(logger, "component", "client", "host", cfg.URL.Host),
		cfg:             cfg,
		entries:         make(chan api.Entry),
		metrics:         metrics,
		streamLagLabels: streamLagLabels,
		name:            asSha256(cfg),

		externalLabels: cfg.ExternalLabels.LabelSet,
		ctx:            ctx,
		cancel:         cancel,
		maxStreams:     maxStreams,
	}
	if cfg.Name != "" {
		c.name = cfg.Name
	}
	c.wal = newClientWAL(c)

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

	if cfg.WAL.Enabled {
		// First replay WAL
		if err = c.replayWAL(); err != nil {
			level.Error(c.logger).Log("msg", "failed to replay WAL", "err", err)
		}
		// Run the client in WAL-enabled mode
		go c.runWithWAL()
	} else {
		// If WAL is disabled, run the client in normal mode. That is, the dispatch action for when a batch is completed
		// is to send it. And when an entry is added to a batch, it's added as a whole.
		go c.runSendSide(
			c.sendBatch,
			func(b *batch, e api.Entry) error {
				return b.add(e)
			})
	}
	return c, nil
}

// NewWithTripperware creates a new Loki client with a custom tripperware.
func NewWithTripperware(metrics *Metrics, cfg Config, streamLagLabels []string, maxStreams int, logger log.Logger, tp Tripperware) (Client, error) {
	c, err := newClient(metrics, cfg, streamLagLabels, maxStreams, logger)
	if err != nil {
		return nil, err
	}

	if tp != nil {
		c.client.Transport = tp(c.client.Transport)
	}

	return c, nil
}

// replayWAL looks walks the WAL directory. Following the "walDir/clientName/tenantID" structure, if any existing WAL s
// are found, read over them and send all pending entries. Since the client is not running yet, we do not care about the
// segment/batch relation here, and rebuild batches in place. After replaying, this will delete the contents of each WAL
// directory.
func (c *client) replayWAL() error {
	var recordPool = newRecordPool()

	// from wal dir, the structured followed is wal_dir/clientName/tenantID/[segment files...]
	clientBaseWALDir := path.Join(c.cfg.WAL.Dir, c.name)
	// look for the WAL dir
	_, err := os.Stat(clientBaseWALDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("wal directory doesn't exist: %w", err)
	}
	// get tenant directories for the client, since we could have multiple as a result of the tenant pipeline stage
	// Note: Ignoring errors.
	matches, _ := filepath.Glob(clientBaseWALDir + "/*")
	var tenantDirs []string
	for _, match := range matches {
		f, _ := os.Stat(match)
		if f.IsDir() {
			tenantDirs = append(tenantDirs, match)
		}
	}
	// no wal files
	if len(matches) < 1 {
		return nil
	}
	for _, tenantDir := range tenantDirs {
		tenantID := tenantDir[strings.LastIndex(tenantDir, "/")+1:]

		// the way the wal works, it keeps a one segment per batch relation while running. Since we are replaying, we can read the
		// wal at once and re-create the batches for sending
		r, closer, err := wal.NewWalReader(tenantDir, -1)
		if err != nil {
			return err
		}
		defer closer.Close()

		// todo, reduce allocations
		b := newBatch(0)
		seriesRecs := make(map[uint64]model.LabelSet)
		for r.Next() {
			rec := recordPool.GetRecord()
			entry := api.Entry{}
			if err := ingester.DecodeWALRecord(r.Record(), rec); err != nil {
				// this error doesn't need to be fatal, we should maybe just throw out this batch?
				level.Warn(c.logger).Log("msg", "failed to decode a wal record", "err", err)
			}
			for _, series := range rec.Series {
				seriesRecs[uint64(series.Ref)] = util.MapToModelLabelSet(series.Labels.Map())
			}
			for _, samples := range rec.RefEntries {
				if l, ok := seriesRecs[uint64(samples.Ref)]; ok {
					entry.Labels = l
					for _, e := range samples.Entries {
						entry.Entry = e
						// If adding the entry to the batch will increase the size over the max
						// size allowed, we do send the current batch and then create a new one
						if b.sizeBytesAfter(entry) > c.cfg.BatchSize {
							_ = c.sendBatch(tenantID, b)
							new := c.newBatch()
							_ = new.add(entry)
							b = new
							break
						}
						// The max size of the batch isn't reached, so we can add the entry
						_ = b.add(entry)
					}

				}
			}
		}
		// send last batch if there are entries left
		if b.bytes > 0 {
			_ = c.sendBatch(tenantID, b)
		}
		if err = cleanWALDir(tenantDir, c.logger); err != nil {
			level.Error(c.logger).Log("msg", fmt.Sprintf("failed to clean WAL directory: %s", tenantDir), "err", err)
		}
	}
	return nil
}

// cleanWALDir removes all files from a WAL directory.
func cleanWALDir(dir string, logger log.Logger) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		// ignore errors and keep going
		if err := os.Remove(filepath.Join(dir, f.Name())); err != nil {
			level.Error(logger).Log("msg", "failed to delete wal file", "err", err)
		}
	}
	return nil
}

// runWithWAL implements the sending side of the client with a WAL in-between. That is, upon receiving an entry, the size
// of the batches being built is tracked and the entry is written to the WAL. Once the max batch size is reached, of the
// maximum wait to for a batch to be sent, a segment in cut on the WAL. This triggers the reading side of the WAL to send
// all entries read from that segment in a single batch.
func (c *client) runWithWAL() {
	go c.runSendSide(func(tenantID string, b *batch) error {
		wal, err := c.wal.getWAL(tenantID)
		if err != nil {
			// wrap this
			return err
		}
		// Cut a segment, that means the watcher will find the segment has ended and send all entries read from there in
		// a single batch.
		if _, err = wal.NextSegment(); err != nil {
			return fmt.Errorf("failed to cut new segment in wal: %w", err)
		}
		return nil
	}, func(b *batch, e api.Entry) error {
		// When the client is running in WAL-enabled mode, we just use the batches to track the total byte size, and the
		// batch age. Since the age is being tracked as of the moment the batch object is created, we just increment the
		// batch size when an entry is to be added.
		b.bytes += len(e.Line)
		return nil
	})
}

// dispatchBatchFunc is a function type that implements the action taken once a batch is ready to be dispatched.
type dispatchBatchFunc func(tenantID string, b *batch) error

// runSendSide is a generic implementation of the main send loop of client. Upon receiving an entry, it starts packing
// them into batches, and once a specific threshold has been reached such as batch size, or age, the batch is dispatched.
func (c *client) runSendSide(
	dispatchBatch dispatchBatchFunc,
	addEntryToBatch func(b *batch, e api.Entry) error,
) {
	batches := map[string]*batch{}
	entryWriter := NewWALEntryWriter()

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
			_ = dispatchBatch(tenantID, batch)
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

			wal, err := c.wal.getWAL(tenantID)
			if err != nil {
				level.Error(c.logger).Log("msg", "failed to get WAL", "err", err)
				// return here?
				return
			}
			// write to wal
			entryWriter.WriteEntry(e, wal, c.logger)

			// after writing to wal, keep track of batch size to know when to cut the batch. That is either
			// send it, or cut a segment in the wal
			batch, ok := batches[tenantID]

			// If the batch doesn't exist yet, we create a new one with the entry
			if !ok {
				b := c.newBatch()
				batches[tenantID] = b
				_ = addEntryToBatch(b, e)
				// Initialize counters to 0 so the metrics are exported before the first
				// occurrence of incrementing to avoid missing metrics.
				for _, counter := range c.metrics.countersWithTenant {
					counter.WithLabelValues(c.cfg.URL.Host, tenantID).Add(0)
				}
				break
			}

			// If adding the entry to the batch will increase the size over the max
			// size allowed, we do send the current batch and then create a new one
			if batch.sizeBytesAfter(e) > c.cfg.BatchSize {
				_ = dispatchBatch(tenantID, batch)
				new := c.newBatch()
				_ = addEntryToBatch(new, e)
				batches[tenantID] = new
				break
			}

			// The max size of the batch isn't reached, so we can add the entry
			err = addEntryToBatch(batch, e)
			if err != nil {
				level.Error(c.logger).Log("msg", "batch add err", "tenant", tenantID, "error", err)
				c.metrics.droppedBytes.WithLabelValues(c.cfg.URL.Host, tenantID).Add(float64(len(e.Line)))
				c.metrics.droppedEntries.WithLabelValues(c.cfg.URL.Host, tenantID).Inc()
				return
			}
		case <-maxWaitCheck.C:
			// Send all batches whose max wait time has been reached
			for tenantID, batch := range batches {
				if batch.age() < c.cfg.BatchWait {
					continue
				}
				_ = dispatchBatch(tenantID, batch)
				delete(batches, tenantID)
			}
		}
	}
}

func (c *client) newBatch() *batch {
	return newBatch(0)
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

func (c *client) sendBatch(tenantID string, batch *batch) error {
	buf, entriesCount, err := batch.encode()
	if err != nil {
		level.Error(c.logger).Log("msg", "error encoding batch", "error", err)
		return err
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

		if err == nil {
			c.metrics.sentBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)
			c.metrics.sentEntries.WithLabelValues(c.cfg.URL.Host).Add(float64(entriesCount))
			for _, s := range batch.streams {
				lbls, err := parser.ParseMetric(s.Labels)
				if err != nil {
					// is this possible?
					level.Warn(c.logger).Log("msg", "error converting stream label string to label.Labels, cannot update lagging metric", "error", err)
					return err
				}

				//nolint:staticcheck
				lblSet := make(prometheus.Labels)
				for _, lbl := range c.streamLagLabels {
					// label from streamLagLabels may not be found but we still need an empty value
					// so that the prometheus client library doesn't panic on inconsistent label cardinality
					value := ""
					for i := range lbls {
						if lbls[i].Name == lbl {
							value = lbls[i].Value
						}
					}
					lblSet[lbl] = value
				}

				//nolint:staticcheck
				if lblSet != nil {
					// always set host
					lblSet[HostLabel] = c.cfg.URL.Host
					// also set client name since if we have multiple promtail clients configured we will run into a
					// duplicate metric collected with same labels error when trying to hit the /metrics endpoint
					lblSet[ClientLabel] = c.name
					c.metrics.streamLag.With(lblSet).Set(time.Since(s.Entries[len(s.Entries)-1].Timestamp).Seconds())
				}
			}
			return nil
		}
		// we know err != nil

		// Only retry 429s, 500s and connection-level errors.
		if status > 0 && status != 429 && status/100 != 5 {
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
		c.metrics.droppedBytes.WithLabelValues(c.cfg.URL.Host, tenantID).Add(bufBytes)
		c.metrics.droppedEntries.WithLabelValues(c.cfg.URL.Host, tenantID).Add(float64(entriesCount))
	}
	return err
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
	c.wal.Stop()
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

func (c *client) UnregisterLatencyMetric(labels prometheus.Labels) {
	labels[HostLabel] = c.cfg.URL.Host
	c.metrics.streamLag.Delete(labels)
}

func (c *client) Name() string {
	return c.name
}

// stoppable is a small interface to keep track of resources that need cleanup.
type stoppable interface {
	Stop()
}

// clientWAL provides a WAL for a given tenant, handling the creation of it and its corresponding watched. Also, it
// handles a graceful stop for all created resources.
// If WAL support is disabled, this will return a no-op WAL for when requested.
type clientWAL struct {
	client     *client
	tenantWALs map[string]WAL
	watchers   map[string]stoppable
}

// newClientWAL creates a new clientWAL.
func newClientWAL(c *client) clientWAL {
	return clientWAL{
		client:     c,
		tenantWALs: make(map[string]WAL),
		watchers:   make(map[string]stoppable),
	}
}

// getWAL is clientWAL main method. Given a tenantID, it retrieves it's corresponding WAL. If none exists, it creates
// a new one, and launches a watcher that listens to entries being added, builds batch and sends them when a signaled.
func (c *clientWAL) getWAL(tenantID string) (WAL, error) {
	if w, ok := c.tenantWALs[tenantID]; ok {
		return w, nil
	}
	// No WAL exists, create new one and launch watcher. If WAL is disabled, this just returns a no-op implementation.
	wal, err := newWAL(c.client.logger, c.client.metrics.registerer, c.client.cfg.WAL, c.client.name, tenantID)
	if err != nil {
		level.Error(c.client.logger).Log("msg", "could not start WAL", "err", err)
		return nil, err
	}
	// todo: find a cleaner way to do this
	if c.client.cfg.WAL.Enabled {
		consumer := newClientConsumer(func(b *batch) error {
			return c.client.sendBatch(tenantID, b)
		}, wal, c.client.logger)
		watcher := NewWALWatcher(wal.Dir(), consumer, c.client.logger)
		watcher.Start()
		c.watchers[tenantID] = watcher
		c.tenantWALs[tenantID] = wal
	}
	return wal, nil
}

func (c *clientWAL) Stop() {
	for _, watcher := range c.watchers {
		watcher.Stop()
	}
}

type sendBatchFunc func(*batch) error

type SegmentDeleter interface {
	DeleteSegment(segmentNum int) error
}

// clientConsumer implements a WatcherConsumer that builds batches with the entries read from the WAL. When the watcher
// signals that a new segment is found, that means the WAL-writing side decided the entries are enough for a batch to be
// sent, therefore, the current batch is sent. After the batch is sent successfully, the previous segment can be cleaned
// up.
type clientConsumer struct {
	series         map[uint64]model.LabelSet
	logger         log.Logger
	currentBatch   *batch
	sendBatch      sendBatchFunc
	segmentDeleter SegmentDeleter
}

func newClientConsumer(sendBatch sendBatchFunc, segmentDeleter SegmentDeleter, logger log.Logger) *clientConsumer {
	return &clientConsumer{
		series:         map[uint64]model.LabelSet{},
		logger:         logger,
		currentBatch:   newBatch(0),
		sendBatch:      sendBatch,
		segmentDeleter: segmentDeleter,
	}
}

func (c *clientConsumer) ConsumeSeries(series record.RefSeries) error {
	c.series[uint64(series.Ref)] = util.MapToModelLabelSet(series.Labels.Map())
	return nil
}

func (c *clientConsumer) ConsumeEntries(samples ingester.RefEntries) error {
	var entry api.Entry
	if l, ok := c.series[uint64(samples.Ref)]; ok {
		entry.Labels = l
		for _, e := range samples.Entries {
			entry.Entry = e
			// Using replay since we know the batch needs to be sent once the segment ends
			c.currentBatch.replay(entry)
		}
	} else {
		// if series is not present for sample, just log for now
		level.Debug(c.logger).Log("series for sample not found")
	}
	return nil
}

func (c *clientConsumer) SegmentEnd(segmentNum int) {
	if err := c.sendBatch(c.currentBatch); err == nil {
		// once the batch has been sent, delete segment if no error
		level.Debug(c.logger).Log("msg", "batch sent successfully. Deleting segment", "segmentNum", segmentNum)
		if err := c.segmentDeleter.DeleteSegment(segmentNum); err != nil {
			level.Error(c.logger).Log("msg", "failed to delete segment after sending batch", "segmentNum", segmentNum)
		}
	}
}
