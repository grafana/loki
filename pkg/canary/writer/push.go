package writer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/build"
)

const (
	defaultContentType         = "application/x-protobuf"
	defaultMaxReponseBufferLen = 1024

	pushEndpoint = "/loki/api/v1/push"
)

var (
	defaultUserAgent = fmt.Sprintf("canary-push/%s", build.GetVersion().Version)
)

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

	// channel for incoming logs
	entries chan entry

	// shutdown channels
	quit chan struct{}
	done chan struct{}

	// auth
	username, password string

	// Will add these label to the logs pushed to loki
	labelName, labelValue, streamName, streamValue string

	// push retry and backoff
	backoff *backoff.Config
}

// NewPush creates an instance of `Push` which writes logs directly to given `lokiAddr`
func NewPush(
	lokiAddr, tenantID string,
	timeout time.Duration,
	cfg config.HTTPClientConfig,
	labelName, labelValue string,
	streamName, streamValue string,
	useTLS bool,
	tlsCfg *tls.Config,
	caFile, certFile, keyFile string,
	username, password string,
	backoffCfg *backoff.Config,
	logger log.Logger,
) (*Push, error) {

	client, err := config.NewClientFromConfig(cfg, "canary-push", config.WithHTTP2Disabled())
	if err != nil {
		return nil, err
	}

	client.Timeout = timeout
	scheme := "http"

	// setup tls transport
	if tlsCfg != nil {
		tlsSettings := config.TLSRoundTripperSettings{
			CA:   config.NewFileSecret(caFile),
			Cert: config.NewFileSecret(certFile),
			Key:  config.NewFileSecret(keyFile),
		}
		rt, err := config.NewTLSRoundTripper(tlsCfg, tlsSettings, func(tls *tls.Config) (http.RoundTripper, error) {
			return &http.Transport{TLSClientConfig: tls}, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config for transport: %w", err)
		}
		client.Transport = rt
		scheme = "https"
	}

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
		logger:      logger,
		entries:     make(chan entry, 50), // Use a buffered channel so we can retry failed pushes without blocking WriteEntry
		quit:        make(chan struct{}),
		done:        make(chan struct{}),
		labelName:   labelName,
		labelValue:  labelValue,
		streamName:  streamName,
		streamValue: streamValue,
		username:    username,
		password:    password,
		backoff:     backoffCfg,
	}
	go p.run()
	return p, nil
}

type entry struct {
	ts    time.Time
	entry string
}

// WriteEntry implements EntryWriter
func (p *Push) WriteEntry(ts time.Time, e string) {
	p.entries <- entry{ts, e}
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
func (p *Push) buildPayload(e entry) ([]byte, error) {

	labels := model.LabelSet{
		model.LabelName(p.labelName):  model.LabelValue(p.labelValue),
		model.LabelName(p.streamName): model.LabelValue(p.streamValue),
	}

	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: labels.String(),
				Entries: []logproto.Entry{
					{
						Timestamp: e.ts,
						Line:      e.entry,
					},
				},
				Hash: uint64(labels.Fingerprint()),
			},
		},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal payload to json: %w", err)
	}

	payload = snappy.Encode(nil, payload)

	return payload, nil
}

// run pulls lines out of the channel and sends them to Loki
func (p *Push) run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		close(p.done)
	}()

	for {
		select {
		case <-p.quit:
			cancel()
			return
		case e := <-p.entries:
			payload, err := p.buildPayload(e)
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
					break
				}

				if status > 0 && status != 429 && status/100 != 5 {
					level.Error(p.logger).Log("msg", "failed to send entry, server rejected push with a non-retryable status code", "entry", e.ts.UnixNano(), "status", status, "err", err)
					break
				}

				if !backoff.Ongoing() {
					level.Error(p.logger).Log("msg", "failed to send entry, retries exhausted, entry will be dropped", "entry", e.ts.UnixNano(), "status", status, "error", err)
					break
				}
				level.Warn(p.logger).Log("msg", "failed to send entry, retrying", "entry", e.ts.UnixNano(), "status", status, "error", err)
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
