package client

import (
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/wal"
)

// Manager manages remote write client instantiation, and connects the related components to orchestrate the flow of api.Entry
// from the scrape targets, to the remote write clients themselves.
//
// Right now it just supports instantiating the WAL writer side of the future-to-be WAL enabled client. In follow-up
// work, tracked in https://github.com/grafana/loki/issues/8197, this Manager will be responsible for instantiating all client
// types: Logger, Multi and WAL.
type Manager struct {
	clientChannelHandler api.EntryHandler
}

func NewManager(reg prometheus.Registerer, logger log.Logger, walCfg wal.Config) (*Manager, error) {
	// TODO: refactor this to instantiate all clients types
	pWAL, err := wal.New(walCfg, logger, reg)
	if err != nil {
		return nil, err
	}
	walWriter := wal.NewWriter(pWAL, logger)

	return &Manager{
		clientChannelHandler: walWriter,
	}, nil
}

func (m *Manager) StopNow() {
	m.clientChannelHandler.Stop()
}

func (m *Manager) Name() string {
	return "wal"
}

func (m *Manager) Chan() chan<- api.Entry {
	return m.clientChannelHandler.Chan()
}

func (m *Manager) Stop() {
	m.clientChannelHandler.Stop()
}
