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
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/golang/snappy"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/build"

	"github.com/grafana/dskit/backoff"

	"github.com/gogo/protobuf/proto"
)

const (
	defaultContentType         = "application/x-protobuf"
	defaultMaxReponseBufferLen = 1024

	pushEndpoint = "/loki/api/v1/push"
)

var defaultUserAgent = fmt.Sprintf("pattern-ingester-push/%s", build.GetVersion().Version)

type EntryWriter interface {
	// WriteEntry handles sending the log to the output
	// To maintain consistent log timing, Write is expected to be non-blocking
	WriteEntry(ts time.Time, entry string, lbls labels.Labels)
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

	// shutdown channels
	quit chan struct{}

	// auth
	username, password string

	// Will add these label to the logs pushed to loki
	labelName, labelValue, streamName, streamValue string

	// push retry and backoff
	backoff *backoff.Config

	entries entries

	metrics *Metrics
}

type entry struct {
	ts     time.Time
	entry  string
	labels labels.Labels
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
	registrer prometheus.Registerer,
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
		metrics: NewMetrics(registrer),
	}

	go p.run(pushPeriod)
	return p, nil
}

// WriteEntry implements EntryWriter
func (p *Push) WriteEntry(ts time.Time, e string, lbls labels.Labels) {
	p.entries.add(entry{ts: ts, entry: e, labels: lbls})
}

// Stop will cancel any ongoing requests and stop the goroutine listening for requests
func (p *Push) Stop() {
	if p.quit != nil {
		close(p.quit)
		p.quit = nil
	}
}

// buildPayload creates the snappy compressed protobuf to send to Loki
func (p *Push) buildPayload(ctx context.Context) ([]byte, error) {
	sp, _ := opentracing.StartSpanFromContext(
		ctx,
		"patternIngester.aggregation.Push.buildPayload",
	)
	defer sp.Finish()

	entries := p.entries.reset()

	entriesByStream := make(map[string][]logproto.Entry)
	for _, e := range entries {
		stream := e.labels.String()
		entries, ok := entriesByStream[stream]
		if !ok {
			entries = make([]logproto.Entry, 0)
		}

		entries = append(entries, logproto.Entry{
			Timestamp: e.ts,
			Line:      e.entry,
		})
		entriesByStream[stream] = entries
	}

	streams := make([]logproto.Stream, 0, len(entriesByStream))

	// limit the number of services to log to 1000
	serviceLimit := len(entriesByStream)
	if serviceLimit > 1000 {
		serviceLimit = 1000
	}

	services := make([]string, 0, serviceLimit)
	for s, entries := range entriesByStream {
		lbls, err := syntax.ParseLabels(s)
		if err != nil {
			continue
		}

		streams = append(streams, logproto.Stream{
			Labels:  s,
			Entries: entries,
			Hash:    lbls.Hash(),
		})

		if len(services) < serviceLimit {
			services = append(services, lbls.Get(push.AggregatedMetricLabel))
		}
	}

	req := &logproto.PushRequest{
		Streams: streams,
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal payload to json: %w", err)
	}

	payload = snappy.Encode(nil, payload)

	p.metrics.streamsPerPush.WithLabelValues(p.tenantID).Observe(float64(len(streams)))
	p.metrics.entriesPerPush.WithLabelValues(p.tenantID).Observe(float64(len(entries)))
	p.metrics.servicesTracked.WithLabelValues(p.tenantID).Set(float64(serviceLimit))

	sp.LogKV(
		"event", "build aggregated metrics payload",
		"num_service", len(entriesByStream),
		"first_1k_services", strings.Join(services, ","),
		"num_streams", len(streams),
		"num_entries", len(entries),
	)

	return payload, nil
}

// run pulls lines out of the channel and sends them to Loki
func (p *Push) run(pushPeriod time.Duration) {
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
			payload, err := p.buildPayload(ctx)
			if err != nil {
				level.Error(p.logger).Log("msg", "failed to build payload", "err", err)
				continue
			}

			// We will use a timeout within each attempt to send
			backoff := backoff.New(context.Background(), *p.backoff)

			// send log with retry
			for {
				status := 0
				status, err = p.send(ctx, payload)
				if err == nil {
					pushTicker.Reset(pushPeriod)
					break
				}

				if status > 0 && util.IsRateLimited(status) && !util.IsServerError(status) {
					level.Error(p.logger).Log("msg", "failed to send entry, server rejected push with a non-retryable status code", "status", status, "err", err)
					pushTicker.Reset(pushPeriod)
					break
				}

				if !backoff.Ongoing() {
					level.Error(p.logger).Log("msg", "failed to send entry, retries exhausted, entry will be dropped", "entry", "status", status, "error", err)
					pushTicker.Reset(pushPeriod)
					break
				}
				level.Warn(p.logger).
					Log("msg", "failed to send entry, retrying", "entry", "status", status, "error", err)
				backoff.Wait()
			}

		}
	}
}

// send makes one attempt to send the payload to Loki
func (p *Push) send(ctx context.Context, payload []byte) (int, error) {
	var (
		err  error
		resp *http.Response
	)

	// Set a timeout for the request
	ctx, cancel := context.WithTimeout(ctx, p.httpClient.Timeout)
	defer cancel()

	sp, ctx := opentracing.StartSpanFromContext(ctx, "patternIngester.aggregation.Push.send")
	defer sp.Finish()

	req, err := http.NewRequestWithContext(ctx, "POST", p.lokiURL, bytes.NewReader(payload))
	p.metrics.payloadSize.WithLabelValues(p.tenantID).Observe(float64(len(payload)))

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
	service string,
	lbls labels.Labels,
) string {
	byteString := util.HumanizeBytes(totalBytes)
	base := fmt.Sprintf(
		"ts=%d bytes=%s count=%d %s=\"%s\"",
		ts.UnixNano(),
		byteString,
		totalCount,
		push.LabelServiceName, service,
	)

	for _, l := range lbls {
		base += fmt.Sprintf(" %s=\"%s\"", l.Name, l.Value)
	}

	return base
}
