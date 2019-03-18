package client

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
)

const contentType = "application/x-protobuf"
const maxErrMsgLen = 1024

var (
	sentBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "sent_bytes_total",
		Help:      "Number of bytes sent.",
	})
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "request_duration_seconds",
		Help:      "Duration of send requests.",
	}, []string{"status_code"})
)

func init() {
	prometheus.MustRegister(sentBytes)
	prometheus.MustRegister(requestDuration)
}

// Config describes configuration for a HTTP pusher client.
type Config struct {
	URL       flagext.URLValue
	BatchWait time.Duration
	BatchSize int

	BackoffConfig  util.BackoffConfig `yaml:"backoff_config"`
	ExternalLabels model.LabelSet     `yaml:"external_labels,omitempty"`
	Timeout        time.Duration      `yaml:"timeout"`
}

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(flags *flag.FlagSet) {
	flags.Var(&c.URL, "client.url", "URL of log server")
	flags.DurationVar(&c.BatchWait, "client.batch-wait", 1*time.Second, "Maximum wait period before sending batch.")
	flags.IntVar(&c.BatchSize, "client.batch-size-bytes", 100*1024, "Maximum batch size to accrue before sending. ")

	flag.IntVar(&c.BackoffConfig.MaxRetries, "client.max-retries", 5, "Maximum number of retires when sending batches.")
	flag.DurationVar(&c.BackoffConfig.MinBackoff, "client.min-backoff", 100*time.Millisecond, "Initial backoff time between retries.")
	flag.DurationVar(&c.BackoffConfig.MaxBackoff, "client.max-backoff", 5*time.Second, "Maximum backoff time between retries.")
	flag.DurationVar(&c.Timeout, "client.timeout", 10*time.Second, "Maximum time to wait for server to respond to a request")
}

// Client for pushing logs in snappy-compressed protos over HTTP.
type Client struct {
	logger  log.Logger
	cfg     Config
	quit    chan struct{}
	entries chan entry
	wg      sync.WaitGroup

	externalLabels model.LabelSet
}

type entry struct {
	labels model.LabelSet
	logproto.Entry
}

// New makes a new Client.
func New(cfg Config, logger log.Logger) (*Client, error) {
	c := &Client{
		logger:  logger,
		cfg:     cfg,
		quit:    make(chan struct{}),
		entries: make(chan entry),

		externalLabels: cfg.ExternalLabels,
	}
	c.wg.Add(1)
	go c.run()
	return c, nil
}

func (c *Client) run() {
	batch := map[model.Fingerprint]*logproto.Stream{}
	batchSize := 0
	maxWait := time.NewTimer(c.cfg.BatchWait)

	defer func() {
		c.sendBatch(batch)
		c.wg.Done()
	}()

	for {
		maxWait.Reset(c.cfg.BatchWait)
		select {
		case <-c.quit:
			return

		case e := <-c.entries:
			if batchSize+len(e.Line) > c.cfg.BatchSize {
				c.sendBatch(batch)
				batchSize = 0
				batch = map[model.Fingerprint]*logproto.Stream{}
			}

			batchSize += len(e.Line)
			fp := e.labels.FastFingerprint()
			stream, ok := batch[fp]
			if !ok {
				stream = &logproto.Stream{
					Labels: e.labels.String(),
				}
				batch[fp] = stream
			}
			stream.Entries = append(stream.Entries, e.Entry)

		case <-maxWait.C:
			if len(batch) > 0 {
				c.sendBatch(batch)
				batchSize = 0
				batch = map[model.Fingerprint]*logproto.Stream{}
			}
		}
	}
}

func (c *Client) sendBatch(batch map[model.Fingerprint]*logproto.Stream) {
	buf, err := encodeBatch(batch)
	if err != nil {
		level.Error(c.logger).Log("msg", "error encoding batch", "error", err)
		return
	}
	sentBytes.Add(float64(len(buf)))

	ctx := context.Background()
	backoff := util.NewBackoff(ctx, c.cfg.BackoffConfig)
	var status int
	for backoff.Ongoing() {
		start := time.Now()
		status, err = c.send(ctx, buf)
		requestDuration.WithLabelValues(strconv.Itoa(status)).Observe(time.Since(start).Seconds())

		if err == nil {
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
	}
}

func encodeBatch(batch map[model.Fingerprint]*logproto.Stream) ([]byte, error) {
	req := logproto.PushRequest{
		Streams: make([]*logproto.Stream, 0, len(batch)),
	}
	for _, stream := range batch {
		req.Streams = append(req.Streams, stream)
	}
	buf, err := proto.Marshal(&req)
	if err != nil {
		return nil, err
	}
	buf = snappy.Encode(nil, buf)
	return buf, nil
}

func (c *Client) send(ctx context.Context, buf []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequest("POST", c.cfg.URL.String(), bytes.NewReader(buf))
	if err != nil {
		return -1, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
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

// Stop the client.
func (c *Client) Stop() {
	close(c.quit)
	c.wg.Wait()
}

// Handle implement EntryHandler; adds a new line to the next batch; send is async.
func (c *Client) Handle(ls model.LabelSet, t time.Time, s string) error {
	if len(c.externalLabels) > 0 {
		ls = c.externalLabels.Merge(ls)
	}

	c.entries <- entry{ls, logproto.Entry{
		Timestamp: t,
		Line:      s,
	}}
	return nil
}
