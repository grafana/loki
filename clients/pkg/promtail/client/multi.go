package client

import (
	"errors"
	"sync"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/promtail/api"
)

// MultiClient is client pushing to one or more loki instances.
type MultiClient struct {
	clients []Client
	entries chan api.Entry
	wg      sync.WaitGroup

	once sync.Once
}

// NewMulti creates a new client
func NewMulti(reg prometheus.Registerer, logger log.Logger, cfgs ...Config) (Client, error) {
	if len(cfgs) == 0 {
		return nil, errors.New("at least one client config should be provided")
	}

	clients := make([]Client, 0, len(cfgs))
	for _, cfg := range cfgs {
		client, err := New(reg, cfg, logger)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	multi := &MultiClient{
		clients: clients,
		entries: make(chan api.Entry),
	}
	multi.start()
	return multi, nil
}

func (m *MultiClient) start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for e := range m.entries {
			for _, c := range m.clients {
				c.Chan() <- e
			}
		}
	}()
}

func (m *MultiClient) Chan() chan<- api.Entry {
	return m.entries
}

// Stop implements Client
func (m *MultiClient) Stop() {
	m.once.Do(func() { close(m.entries) })
	m.wg.Wait()
	for _, c := range m.clients {
		c.Stop()
	}
}

// StopNow implements Client
func (m *MultiClient) StopNow() {
	for _, c := range m.clients {
		c.StopNow()
	}
}
