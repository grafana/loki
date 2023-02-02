package client

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/go-kit/log"

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
func NewMulti(metrics *Metrics, logger log.Logger, maxStreams, maxLineSize int, maxLineSizeTruncate bool, cfgs ...Config) (Client, error) {
	var fake struct{}

	if len(cfgs) == 0 {
		return nil, errors.New("at least one client config should be provided")
	}
	clientsCheck := make(map[string]struct{})
	clients := make([]Client, 0, len(cfgs))
	for _, cfg := range cfgs {
		client, err := New(metrics, cfg, maxStreams, maxLineSize, maxLineSizeTruncate, logger)
		if err != nil {
			return nil, err
		}

		// Don't allow duplicate clients, we have client specific metrics that need at least one unique label value (name).
		if _, ok := clientsCheck[client.Name()]; ok {
			return nil, fmt.Errorf("duplicate client configs are not allowed, found duplicate for URL: %s", cfg.URL)
		}

		clientsCheck[client.Name()] = fake
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

func (m *MultiClient) Name() string {
	var sb strings.Builder
	sb.WriteString("multi:")
	for i, c := range m.clients {
		sb.WriteString(c.Name())
		if i != len(m.clients)-1 {
			sb.WriteString(",")
		}
	}
	return sb.String()
}
