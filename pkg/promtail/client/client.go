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
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

const contentType = "application/x-protobuf"
const maxErrMsgLen = 1024

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
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "request_duration_seconds",
		Help:      "Duration of send requests.",
	}, []string{"status_code", "host"})
)

func init() {
	prometheus.MustRegister(encodedBytes)
	prometheus.MustRegister(sentBytes)
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
	labels model.LabelSet
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

	c.client, err = config.NewClientFromConfig(cfg.Client, "promtail")
	if err != nil {
		return nil, err
	}

	c.client.Timeout = cfg.Timeout

	c.wg.Add(1)
	go c.run()
	return c, nil
}

func (c *client) run() {
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

func (c *client) sendBatch(batch map[model.Fingerprint]*logproto.Stream) {
	buf, err := encodeBatch(batch)
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
		status, err = c.send(ctx, buf)
		requestDuration.WithLabelValues(strconv.Itoa(status), c.cfg.URL.Host).Observe(time.Since(start).Seconds())

		if err == nil {
			sentBytes.WithLabelValues(c.cfg.URL.Host).Add(bufBytes)
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

func (c *client) send(ctx context.Context, buf []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequest("POST", c.cfg.URL.String(), bytes.NewReader(buf))
	if err != nil {
		return -1, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", contentType)

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

	c.entries <- entry{ls, logproto.Entry{
		Timestamp: t,
		Line:      s,
	}}
	return nil
}
