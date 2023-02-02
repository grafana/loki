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
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/build"
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
		rt, err := config.NewTLSRoundTripper(tlsCfg, caFile, certFile, keyFile, func(tls *tls.Config) (http.RoundTripper, error) {
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

	return &Push{
		lokiURL:     u.String(),
		tenantID:    tenantID,
		httpClient:  client,
		userAgent:   defaultUserAgent,
		contentType: defaultContentType,
		logger:      logger,
		labelName:   labelName,
		labelValue:  labelValue,
		streamName:  streamName,
		streamValue: streamValue,
		username:    username,
		password:    password,
		backoff:     backoffCfg,
	}, nil
}

// Write implements the io.Writer.
func (p *Push) Write(payload []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.httpClient.Timeout)
	defer cancel()
	if err := p.send(ctx, payload); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (p *Push) parsePayload(payload []byte) (*logproto.PushRequest, error) {
	// payload that is sent by the `writer` will be in format `LogEntry`
	var (
		tsStr, logLine string
	)
	if _, err := fmt.Sscanf(string(payload), LogEntry, &tsStr, &logLine); err != nil {
		return nil, fmt.Errorf("failed to parse payload written sent by writer: %w", err)
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unix nano timestamp: %w", err)
	}

	labels := model.LabelSet{
		model.LabelName(p.labelName):  model.LabelValue(p.labelValue),
		model.LabelName(p.streamName): model.LabelValue(p.streamValue),
	}

	return &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: labels.String(),
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, ts),
						Line:      string(payload),
					},
				},
				Hash: uint64(labels.Fingerprint()),
			},
		},
	}, nil
}

// send does the heavy lifting of sending the generated logs into the Loki server.
// It won't batch.
func (p *Push) send(ctx context.Context, payload []byte) error {
	var (
		resp *http.Response
		err  error
	)

	preq, err := p.parsePayload(payload)
	if err != nil {
		return err
	}

	payload, err = proto.Marshal(preq)
	if err != nil {
		return fmt.Errorf("failed to marshal payload to json: %w", err)
	}

	payload = snappy.Encode(nil, payload)

	req, err := http.NewRequest("POST", p.lokiURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create push request: %w", err)
	}
	req = req.WithContext(ctx)
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

	backoff := backoff.New(ctx, *p.backoff)

	// send log with retry
	for {
		resp, err = p.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to push payload: %w", err)
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

		if status > 0 && status != 429 && status/100 != 5 {
			break
		}

		if !backoff.Ongoing() {
			break
		}

		level.Info(p.logger).Log("msg", "retrying as server returned non successful error", "status", status, "error", err)

	}

	return err
}
