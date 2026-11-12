package aggregation

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/otel"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/build"

	"github.com/grafana/dskit/backoff"
)

var tracer = otel.Tracer("pkg/pattern/aggregation")

const (
	defaultContentType         = "application/x-protobuf"
	defaultMaxBatchSize        = 8 << 20
	defaultMaxReponseBufferLen = 1024
	pushEndpoint               = "/loki/api/v1/push"
)

var defaultUserAgent = fmt.Sprintf("pattern-ingester-push/%s", build.GetVersion().Version)

type EntryWriter interface {
	// WriteEntry handles sending the log to the output
	// To maintain consistent log timing, Write is expected to be non-blocking
	WriteEntry(ts time.Time, e string, lbls labels.Labels, structuredMetadata []logproto.LabelAdapter)
	Stop()
}

// Push is a io.Writer, that writes given log entries by pushing
// directly to the given loki server URL. Each `Push` instance handles for a single tenant.
// No batching of log lines happens when sending to Loki.
type Push struct {
	lokiURL     string
	tenantID    string
	httpClient  *http.Client
	userAgent   string
	contentType string
	logger      log.Logger

	running  sync.WaitGroup
	quit     chan struct{}
	quitOnce sync.Once

	// auth
	username, password string

	// push retry and backoff
	backoff *backoff.Config

	entries      entries
	maxBatchSize int

	metrics *Metrics
}

type entry struct {
	ts                 time.Time
	entry              string
	labels             labels.Labels
	structuredMetadata []logproto.LabelAdapter
}

type entries struct {
	lock    sync.Mutex
	entries []entry
}

func (e *entries) add(entry entry) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.entries = append(e.entries, entry)
}

func (e *entries) reset() []entry {
	e.lock.Lock()
	defer e.lock.Unlock()
	entries := e.entries
	e.entries = make([]entry, 0, len(entries))
	return entries
}

// NewPush creates an instance of `Push` which writes logs directly to given `lokiAddr`
func NewPush(
	lokiAddr, tenantID string,
	timeout time.Duration,
	pushPeriod time.Duration,
	cfg config.HTTPClientConfig,
	username, password string,
	useTLS bool,
	backoffCfg *backoff.Config,
	logger log.Logger,
	metrics *Metrics,
) (*Push, error) {
	client, err := config.NewClientFromConfig(cfg, "pattern-ingester-push", config.WithHTTP2Disabled())
	if err != nil {
		return nil, err
	}

	client.Timeout = timeout
	scheme := "http"

	// setup tls transport
	if useTLS {
		scheme = "https"
	}

	u := url.URL{
		Scheme: scheme,
		Host:   lokiAddr,
		Path:   pushEndpoint,
	}

	p := &Push{
		lokiURL:     u.String(),
		tenantID:    tenantID,
		httpClient:  client,
		userAgent:   defaultUserAgent,
		contentType: defaultContentType,
		username:    username,
		password:    password,
		logger:      logger,
		quit:        make(chan struct{}),
		backoff:     backoffCfg,
		entries: entries{
			entries: make([]entry, 0),
		},
		metrics: metrics,
	}

	p.running.Add(1)
	go p.run(pushPeriod)

	level.Debug(p.logger).Log("msg", "started pushing aggregation entries")
	return p, nil
}

// WriteEntry implements EntryWriter
func (p *Push) WriteEntry(ts time.Time, e string, lbls labels.Labels, structuredMetadata []logproto.LabelAdapter) {
	p.entries.add(entry{ts: ts, entry: e, labels: lbls, structuredMetadata: structuredMetadata})
}

// Stop will cancel any ongoing requests and stop the goroutine listening for requests
func (p *Push) Stop() {
	p.quitOnce.Do(func() {
		close(p.quit)
	})
	p.running.Wait()
}

// run pulls lines out of the channel and sends them to Loki
func (p *Push) run(pushPeriod time.Duration) {
	defer p.running.Done()

	ctx, cancel := context.WithCancel(context.Background())
	pushTicker := time.NewTimer(pushPeriod)
	defer pushTicker.Stop()

	defer func() {
		pushTicker.Stop()
	}()

	for {
		select {
		case <-p.quit:
			cancel()
			return
		case <-pushTicker.C:
			pushTicker.Reset(pushPeriod)
			batches := p.buildBatches()
			if len(batches) == 0 {
				level.Warn(p.logger).Log("msg", "skipping aggregation push, payload is empty")
				continue
			}

			// Each batch is encoded and sent on its own, so only one encoded batch
			// is held in memory at a time, and a batch that fails does not stop the
			// ones after it from being sent.
			for _, b := range batches {
				body, err := p.encode(b)
				if err != nil {
					level.Error(p.logger).Log("msg", "failed to build payload", "err", err)
					continue
				}
				if body == nil {
					level.Warn(p.logger).Log("msg", "skipping aggregation push, batch has no streams with valid labels")
					continue
				}
				p.sendWithBackoff(ctx, body)
			}
		}
	}
}

// sendWithBackoff sends the body to Loki and retries it until it succeeds,
// the server rejects it with a status that is not retried, or the retries are
// exhausted. In the last two cases the body is dropped.
func (p *Push) sendWithBackoff(ctx context.Context, body []byte) {
	backoff := backoff.New(context.Background(), *p.backoff)
	for {
		status, err := p.send(ctx, body)
		if err == nil {
			level.Info(p.logger).Log("msg", "successfully pushed aggregation entry", "status", status)
			return
		}

		if status > 0 && util.IsRateLimited(status) && !util.IsServerError(status) {
			level.Error(p.logger).Log("msg", "failed to send entry, server rejected push with a non-retryable status code", "status", status, "err", err)
			return
		}

		if !backoff.Ongoing() {
			level.Error(p.logger).Log("msg", "failed to send entry, retries exhausted, entry will be dropped", "status", status, "error", err)
			return
		}
		level.Warn(p.logger).Log("msg", "failed to send entry, retrying", "status", status, "error", err)
		backoff.Wait()
	}
}

// send makes one attempt to send the body to Loki
func (p *Push) send(ctx context.Context, body []byte) (int, error) {
	var (
		err  error
		resp *http.Response
	)

	// Set a timeout for the request
	ctx, cancel := context.WithTimeout(ctx, p.httpClient.Timeout)
	defer cancel()

	ctx, sp := tracer.Start(ctx, "patternIngester.aggregation.Push.send")
	defer sp.End()

	req, err := http.NewRequestWithContext(ctx, "POST", p.lokiURL, bytes.NewReader(body))
	p.metrics.payloadSize.WithLabelValues(p.tenantID).Observe(float64(len(body)))

	if err != nil {
		return -1, fmt.Errorf("failed to create push request: %w", err)
	}
	req.Header.Set("Content-Type", p.contentType)
	req.Header.Set("User-Agent", p.userAgent)

	// set org-id
	if p.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", p.tenantID)
	}

	// basic auth if provided
	if p.username != "" {
		req.SetBasicAuth(p.username, p.password)
	}

	resp, err = p.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			p.metrics.writeTimeout.WithLabelValues(p.tenantID).Inc()
		}
		return -1, fmt.Errorf("failed to push payload: %w", err)
	}
	statusCode := resp.StatusCode
	if util.IsError(statusCode) {
		errType := util.ErrorTypeFromHTTPStatus(statusCode)

		scanner := bufio.NewScanner(io.LimitReader(resp.Body, defaultMaxReponseBufferLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, statusCode, line)
		p.metrics.pushErrors.WithLabelValues(p.tenantID, errType).Inc()
	}

	if err := resp.Body.Close(); err != nil {
		level.Error(p.logger).Log("msg", "failed to close response body", "error", err)
	}

	return statusCode, err
}

func AggregatedMetricEntry(
	ts model.Time,
	totalBytes, totalCount uint64,
	lbls labels.Labels,
) string {
	byteString := util.HumanizeBytes(totalBytes)
	base := fmt.Sprintf(
		"ts=%d bytes=%s count=%d",
		ts.UnixNano(),
		byteString,
		totalCount,
	)

	return internalEntry(base, lbls)
}

func PatternEntry(
	ts time.Time,
	count int64,
	pattern string,
	lbls labels.Labels,
) string {
	base := fmt.Sprintf(
		`ts=%d count=%d detected_pattern="%s"`,
		ts.UnixNano(),
		count,
		url.QueryEscape(pattern),
	)

	return internalEntry(base, lbls)
}

func internalEntry(
	base string,
	lbls labels.Labels,
) string {
	lbls.Range(func(l labels.Label) {
		base += fmt.Sprintf(" %s=\"%s\"", l.Name, l.Value)
	})

	return base
}
