package kafka

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

const (
	WorkerLabel = "worker"
)

var DefaultClientFactory = client.NewMulti

type fanOutHandler struct {
	handlers []api.EntryHandler
	curr     int

	entries chan api.Entry
	wg      sync.WaitGroup

	once sync.Once
}

func NewFanOutHandler(
	workerCount int,
	logger log.Logger,
	reg prometheus.Registerer,
	middlewareFactory func() (api.EntryMiddleware, error),
	clientConfigs ...client.Config,
) (api.EntryHandler, error) {
	if workerCount == 0 {
		return nil, errors.New("worker count must be positive")
	}
	if workerCount == 1 {
		c, err := DefaultClientFactory(reg, logger, clientConfigs...)
		if err != nil {
			return nil, err
		}
		m, err := middlewareFactory()
		if err != nil {
			return nil, err
		}
		h := m.Wrap(c)
		return api.NewEntryHandler(h.Chan(), func() {
			h.Stop()
			c.Stop()
		}), nil
	}
	handlers := make([]api.EntryHandler, workerCount)
	for i := 0; i < workerCount; i++ {
		c, err := DefaultClientFactory(reg, logger, clientConfigs...)
		if err != nil {
			return nil, err
		}
		m, err := middlewareFactory()
		if err != nil {
			return nil, err
		}
		pipeline := m.Wrap(c)
		handler := api.AddLabelsMiddleware(model.LabelSet{WorkerLabel: model.LabelValue(fmt.Sprintf("%d", i))}).Wrap(pipeline)
		handlers[i] = api.NewEntryHandler(handler.Chan(), func() {
			handler.Stop()
			pipeline.Stop()
			c.Stop()
		})
	}
	f := &fanOutHandler{
		handlers: handlers,
		entries:  make(chan api.Entry),
	}
	f.start()
	return f, nil
}

func (m *fanOutHandler) start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for e := range m.entries {
			m.balance().Chan() <- e
		}
	}()
}

func (m *fanOutHandler) balance() api.EntryHandler {
	e := m.handlers[m.curr]
	m.curr++
	if m.curr >= len(m.handlers) {
		m.curr = 0
	}
	return e
}

func (m *fanOutHandler) Chan() chan<- api.Entry {
	return m.entries
}

// Stop implements Client
func (m *fanOutHandler) Stop() {
	m.once.Do(func() { close(m.entries) })
	m.wg.Wait()
	for _, c := range m.handlers {
		c.Stop()
	}
}
