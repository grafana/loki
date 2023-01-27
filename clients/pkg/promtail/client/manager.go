package client

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/wal"
	"github.com/prometheus/client_golang/prometheus"
)

// Manager manages remote write client instantiation, and connects the related components to orchestrate the flow of api.Entry
// from the scrape targets, to the remote write clients themselves.
//
// Right now it just supports instantiating the WAL writer side of the future-to-be WAL enabled client. In follow-up
// work, tracked in https://github.com/grafana/loki/issues/8197, this Manager will be responsible for instantiating all client
// types: Logger, Multi and WAL.
type Manager struct {
	clients []Client

	entries chan api.Entry
	once    sync.Once

	wg sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(metrics *Metrics, logger log.Logger, maxStreams, maxLineSize int, maxLineSizeTruncate bool, reg prometheus.Registerer, walCfg wal.Config, clientCfgs ...Config) (*Manager, error) {
	// TODO: refactor this to instantiate all clients types
	var fake struct{}

	if len(clientCfgs) == 0 {
		return nil, fmt.Errorf("at least one client config should be provided")
	}
	clientsCheck := make(map[string]struct{})
	clients := make([]Client, 0, len(clientCfgs))
	for _, cfg := range clientCfgs {
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

	manager := &Manager{
		clients: clients,
		entries: make(chan api.Entry),
	}
	manager.start()
	return manager, nil
}

func (m *Manager) start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// keep reading received entries
		for e := range m.entries {
			// then fanout to every remote write client
			for _, c := range m.clients {
				c.Chan() <- e
			}
		}
	}()
}

func (m *Manager) StopNow() {
	for _, c := range m.clients {
		c.StopNow()
	}
}

func (m *Manager) Name() string {
	var sb strings.Builder
	// name contains wal since manager is used as client only when WAL enabled for now
	sb.WriteString("wal:")
	for i, c := range m.clients {
		sb.WriteString(c.Name())
		if i != len(m.clients)-1 {
			sb.WriteString(",")
		}
	}
	return sb.String()
}

func (m *Manager) Chan() chan<- api.Entry {
	return m.entries
}

func (m *Manager) Stop() {
	// first stop the receiving channel
	m.once.Do(func() { close(m.entries) })
	m.wg.Wait()
	// close clients
	for _, c := range m.clients {
		c.Stop()
	}
}
