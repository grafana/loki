package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/joncrlsn/dque"
	"github.com/prometheus/common/model"
)

type dqueConfig struct {
	queueDir         string
	queueSegmentSize int
	queueSync        bool
	queueName        string
}

var defaultDqueConfig = dqueConfig{
	queueDir:         "/tmp/flb-storage/loki",
	queueSegmentSize: 500,
	queueSync:        false,
	queueName:        "dque",
}

type dqueEntry struct {
	Lbs  model.LabelSet
	Ts   time.Time
	Line string
}

func dqueEntryBuider() interface{} {
	return &dqueEntry{}
}

type dqueClient struct {
	logger log.Logger
	queue  *dque.DQue
	loki   client.Client
	quit   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

// New makes a new dque loki client
func newDque(cfg *config, logger log.Logger) (client.Client, error) {
	var err error

	q := &dqueClient{
		logger: log.With(logger, "component", "queue", "host", cfg.clientConfig.TenantID),
		quit:   make(chan struct{}),
	}

	err = os.MkdirAll(cfg.bufferConfig.dqueConfig.queueDir, 0644)
	if err != nil {
		return nil, fmt.Errorf("cannot create queue directory: %s", err)
	}

	q.queue, err = dque.NewOrOpen(cfg.bufferConfig.dqueConfig.queueName, cfg.bufferConfig.dqueConfig.queueDir, cfg.bufferConfig.dqueConfig.queueSegmentSize, dqueEntryBuider)
	if err != nil {
		return nil, err
	}

	if !cfg.bufferConfig.dqueConfig.queueSync {
		q.queue.TurboOn()
	}

	q.loki, err = client.New(cfg.clientConfig, logger)
	if err != nil {
		return nil, err
	}

	q.wg.Add(1)
	go q.dequeuer()
	return q, nil
}

func (c *dqueClient) dequeuer() {
	defer func() {
		c.queue.Close()
		c.wg.Done()
	}()

	for {
		select {
		case <-c.quit:
			return
		default:
		}

		// Dequeue the next item in the queue
		entry, err := c.queue.DequeueBlock()
		if err != nil {
			level.Error(c.logger).Log("msg", "error dequeuing record", "error", err)
			continue
		}

		// Assert type of the response to an Item pointer so we can work with it
		record, ok := entry.(*dqueEntry)
		if !ok {
			level.Error(c.logger).Log("msg", "error dequeued record is not an valid type", "error")
			continue
		}

		if err := c.loki.Handle(record.Lbs, record.Ts, record.Line); err != nil {
			level.Error(c.logger).Log("msg", "error sending record to Loki", "error", err)
		}
	}
}

// Stop the client
func (c *dqueClient) Stop() {
	c.once.Do(func() { close(c.quit) })
	c.loki.Stop()
	c.wg.Wait()
}

// Handle implement EntryHandler; adds a new line to the next batch; send is async.
func (c *dqueClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	if err := c.queue.Enqueue(&dqueEntry{ls, t, s}); err != nil {
		return fmt.Errorf("cannot enqueue record %s: %s", s, err)
	}

	return nil
}
