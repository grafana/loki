package client

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/wal"
)

// WriterEventsNotifier implements a notifier that's received by the Manager, to which wal.Watcher can subscribe for
// writer events.
type WriterEventsNotifier interface {
	SubscribeCleanup(subscriber wal.CleanupEventSubscriber)
	SubscribeWrite(subscriber wal.WriteEventSubscriber)
}

type Stoppable interface {
	Stop()
}

// Manager manages remote write client instantiation, and connects the related components to orchestrate the flow of api.Entry
// from the scrape targets, to the remote write clients themselves.
//
// Right now it just supports instantiating the WAL writer side of the future-to-be WAL enabled client. In follow-up
// work, tracked in https://github.com/grafana/loki/issues/8197, this Manager will be responsible for instantiating all client
// types: Logger, Multi and WAL.
type Manager struct {
	clients     []Client
	walWatchers []Stoppable

	entries chan api.Entry
	once    sync.Once

	wg sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(
	metrics *Metrics,
	logger log.Logger,
	maxStreams, maxLineSize int,
	maxLineSizeTruncate bool,
	reg prometheus.Registerer,
	walCfg wal.Config,
	notifier WriterEventsNotifier,
	clientCfgs ...Config,
) (*Manager, error) {
	// TODO: refactor this to instantiate all clients types
	var fake struct{}

	watcherMetrics := wal.NewWatcherMetrics(reg)

	if len(clientCfgs) == 0 {
		return nil, fmt.Errorf("at least one client config should be provided")
	}
	clientsCheck := make(map[string]struct{})
	clients := make([]Client, 0, len(clientCfgs))
	watchers := make([]Stoppable, 0, len(clientCfgs))
	for _, cfg := range clientCfgs {
		client, err := New(metrics, cfg, maxStreams, maxLineSize, maxLineSizeTruncate, logger)
		if err != nil {
			return nil, err
		}

		// Don't allow duplicate clients, we have client specific metrics that need at least one unique label value (name).
		if _, ok := clientsCheck[client.Name()]; ok {
			return nil, fmt.Errorf("duplicate client configs are not allowed, found duplicate for name: %s", cfg.Name)
		}

		clientsCheck[client.Name()] = fake
		clients = append(clients, client)

		// Create and launch wal watcher for this client

		// add some context information for the logger the watcher uses
		wlog := log.With(logger, "client", client.Name())

		writeTo := newClientWriteTo(client.Chan(), wlog)
		// subscribe watcher's wal.WriteTo to writer events. This will make the writer trigger the cleanup of the wal.WriteTo
		// series cache whenever a segment is deleted.
		notifier.SubscribeCleanup(writeTo)

		watcher := wal.NewWatcher(walCfg.Dir, client.Name(), watcherMetrics, writeTo, wlog, walCfg.WatchConfig)
		// subscribe watcher to wal write events
		notifier.SubscribeWrite(watcher)

		level.Debug(logger).Log("msg", "starting WAL watcher for client", "client", client.Name())
		watcher.Start()

		watchers = append(watchers, watcher)
	}

	manager := &Manager{
		clients:     clients,
		walWatchers: watchers,
		entries:     make(chan api.Entry),
	}
	manager.start()
	return manager, nil
}

func (m *Manager) start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// discard read entries
		for range m.entries {
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
	// close wal watchers
	for _, walWatcher := range m.walWatchers {
		walWatcher.Stop()
	}
	// close clients
	for _, c := range m.clients {
		c.Stop()
	}
}
