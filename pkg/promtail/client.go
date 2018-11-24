package promtail

import (
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/grafana/tempo/pkg/logproto"
)

const contentType = "application/x-protobuf"

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

// ClientConfig describes configuration for a HTTP pusher client.
type ClientConfig struct {
	URL       flagext.URLValue
	BatchWait time.Duration
	BatchSize int
}

// RegisterFlags registers flags.
func (c *ClientConfig) RegisterFlags(flags *flag.FlagSet) {
	flags.Var(&c.URL, "client.url", "URL of log server")
	flags.DurationVar(&c.BatchWait, "client.batch-wait", 1*time.Second, "Maximum wait period before sending batch.")
	flags.IntVar(&c.BatchSize, "client.batch-size-bytes", 100*1024, "Maximum batch size to accrue before sending. ")
}

// Client for pushing logs in snappy-compressed protos over HTTP.
type Client struct {
	cfg     ClientConfig
	quit    chan struct{}
	entries chan entry
	wg      sync.WaitGroup
}

type entry struct {
	labels model.LabelSet
	logproto.Entry
}

// NewClient makes a new Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	c := &Client{
		cfg:     cfg,
		quit:    make(chan struct{}),
		entries: make(chan entry),
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
		if err := c.send(batch); err != nil {
			level.Error(util.Logger).Log("msg", "error sending batch", "error", err)
		}
		c.wg.Done()
	}()

	for {
		maxWait.Reset(c.cfg.BatchWait)
		select {
		case <-c.quit:
			return
		case e := <-c.entries:
			if batchSize+len(e.Line) > c.cfg.BatchSize {
				if err := c.send(batch); err != nil {
					log.Errorf("Error sending batch: %v", err)
				}
				batch = map[model.Fingerprint]*logproto.Stream{}
			}

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
				if err := c.send(batch); err != nil {
					log.Errorf("Error sending batch: %v", err)
				}
				batch = map[model.Fingerprint]*logproto.Stream{}
			}
		}
	}
}

func (c *Client) send(batch map[model.Fingerprint]*logproto.Stream) error {
	req := logproto.PushRequest{
		Streams: make([]*logproto.Stream, 0, len(batch)),
	}
	count := 0
	for _, stream := range batch {
		req.Streams = append(req.Streams, stream)
		count += len(stream.Entries)
	}
	buf, err := proto.Marshal(&req)
	if err != nil {
		return err
	}
	buf = snappy.Encode(nil, buf)
	sentBytes.Add(float64(len(buf)))

	start := time.Now()
	resp, err := http.Post(c.cfg.URL.String(), contentType, bytes.NewReader(buf))
	if err != nil {
		requestDuration.WithLabelValues("failed").Observe(time.Until(start).Seconds())
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return err
	}
	requestDuration.WithLabelValues(strconv.Itoa(resp.StatusCode)).Observe(time.Until(start).Seconds())

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Error doing write: %d - %s", resp.StatusCode, resp.Status)
	}
	return nil
}

// Stop the client.
func (c *Client) Stop() {
	close(c.quit)
	c.wg.Wait()
}

// Line adds a new line to the next batch; send is async.
func (c *Client) Line(ls model.LabelSet, t time.Time, s string) error {
	c.entries <- entry{ls, logproto.Entry{
		Timestamp: t,
		Line:      s,
	}}
	return nil
}
