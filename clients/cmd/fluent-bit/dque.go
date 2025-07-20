package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/joncrlsn/dque"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type dqueConfig struct {
	queueDir         string
	queueSegmentSize int
	queueSync        bool
	queueName        string
}

var defaultDqueConfig = dqueConfig{
	queueDir:         filepath.Join(os.TempDir(), "flb-storage/loki"),
	queueSegmentSize: 500,
	queueSync:        false,
	queueName:        "dque",
}

type dqueEntry struct {
	Lbs  model.LabelSet
	Ts   time.Time
	Line string
}

func dqueEntryBuilder() interface{} {
	return &dqueEntry{}
}

type dqueClient struct {
	logger  log.Logger
	queue   *dque.DQue
	loki    client.Client
	once    sync.Once
	wg      sync.WaitGroup
	entries chan api.Entry
}

// New makes a new dque loki client
func newDque(cfg *config, logger log.Logger, metrics *client.Metrics) (client.Client, error) {
	var err error

	q := &dqueClient{
		logger: log.With(logger, "component", "queue", "name", cfg.bufferConfig.dqueConfig.queueName),
	}

	err = os.MkdirAll(cfg.bufferConfig.dqueConfig.queueDir, 0640)
	if err != nil {
		return nil, fmt.Errorf("cannot create queue directory: %s", err)
	}

	q.queue, err = dque.NewOrOpen(cfg.bufferConfig.dqueConfig.queueName, cfg.bufferConfig.dqueConfig.queueDir, cfg.bufferConfig.dqueConfig.queueSegmentSize, dqueEntryBuilder)
	if err != nil {
		return nil, err
	}

	if !cfg.bufferConfig.dqueConfig.queueSync {
		_ = q.queue.TurboOn()
	}

	q.loki, err = client.New(metrics, cfg.clientConfig, 0, 0, false, logger)
	if err != nil {
		return nil, err
	}

	q.entries = make(chan api.Entry)

	q.wg.Add(2)
	go q.enqueuer()
	go q.dequeuer()
	return q, nil
}

func (c *dqueClient) dequeuer() {
	defer c.wg.Done()
	for {
		// Dequeue the next item in the queue
		entry, err := c.queue.DequeueBlock()
		if err != nil {
			switch err {
			case dque.ErrQueueClosed:
				return
			default:
				level.Error(c.logger).Log("msg", "error dequeuing record", "error", err)
				continue
			}
		}

		// Assert type of the response to an Item pointer so we can work with it
		record, ok := entry.(*dqueEntry)
		if !ok {
			level.Error(c.logger).Log("msg", "error dequeued record is not an valid type", "error")
			continue
		}

		c.loki.Chan() <- api.Entry{
			Labels: record.Lbs,
			Entry: logproto.Entry{
				Timestamp: record.Ts,
				Line:      record.Line,
			},
		}
	}
}

// Stop the client
func (c *dqueClient) Stop() {
	c.once.Do(func() {
		close(c.entries)
		c.queue.Close()
		c.loki.Stop()
		c.wg.Wait()
	})

}

func (c *dqueClient) Chan() chan<- api.Entry {
	return c.entries
}

// Stop the client
func (c *dqueClient) StopNow() {
	c.once.Do(func() {
		close(c.entries)
		c.queue.Close()
		c.loki.StopNow()
		c.wg.Wait()
	})
}

func (c *dqueClient) enqueuer() {
	defer c.wg.Done()
	for e := range c.entries {
		if err := c.queue.Enqueue(&dqueEntry{e.Labels, e.Timestamp, e.Line}); err != nil {
			level.Warn(c.logger).Log("msg", fmt.Sprintf("cannot enqueue record %s:", e.Line), "err", err)
		}
	}
}

func (c *dqueClient) Name() string {
	return ""
}
