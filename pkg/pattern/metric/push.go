package metric

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/golang/snappy"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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
	done chan struct{}

	// auth
	username, password string

	// Will add these label to the logs pushed to loki
	labelName, labelValue, streamName, streamValue string

	// push retry and backoff
	backoff *backoff.Config

	entries entries
}

type entry struct {
	ts     time.Time
	entry  string
	labels labels.Labels
}

type entries struct {
	lock    sync.RWMutex
	entries []entry
}

func (e *entries) add(entry entry) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.entries = append(e.entries, entry)
}

func (e *entries) reset() {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.entries = e.entries[:0]
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
		done:        make(chan struct{}),
		backoff:     backoffCfg,
		entries: entries{
			entries: make([]entry, 0),
		},
	}

	// TODO: pass run a ticker
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
		<-p.done
		p.quit = nil
	}
}

// buildPayload creates the snappy compressed protobuf to send to Loki
func (p *Push) buildPayload() ([]byte, error) {
	p.entries.lock.RLock()
	defer p.entries.lock.RUnlock()

	entriesByStream := make(map[string][]logproto.Entry)
	for _, e := range p.entries.entries {
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
	}

	req := &logproto.PushRequest{
		Streams: streams,
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal payload to json: %w", err)
	}

	payload = snappy.Encode(nil, payload)

	return payload, nil
}

// run pulls lines out of the channel and sends them to Loki
func (p *Push) run(pushPeriod time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	pushTicker := time.NewTimer(pushPeriod)
	defer pushTicker.Stop()

	defer func() {
		pushTicker.Stop()
		close(p.done)
	}()

	for {
		select {
		case <-p.quit:
			cancel()
			return
		case <-pushTicker.C:
			payload, err := p.buildPayload()
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
					// reset entries on successful push
					p.entries.reset()
					pushTicker.Reset(pushPeriod)
					break
				}

				if status > 0 && status != 429 && status/100 != 5 {
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
	req, err := http.NewRequestWithContext(ctx, "POST", p.lokiURL, bytes.NewReader(payload))
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
		return -1, fmt.Errorf("failed to push payload: %w", err)
	}
	status := resp.StatusCode
	if status/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, defaultMaxReponseBufferLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, status, line)
	}

	if err := resp.Body.Close(); err != nil {
		level.Error(p.logger).Log("msg", "failed to close response body", "error", err)
	}

	return status, err
}
