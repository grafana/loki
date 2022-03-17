package client

import "github.com/grafana/loki/pkg/ingester"

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	Start()
	// Log marshalls the records and writes it into the WAL.
	Log(*ingester.WALRecord) error
	// Stop stops all the WAL operations.
	Stop() error
}

type noopWAL struct{}

func (n *noopWAL) Start() {}

func (n *noopWAL) Log(*ingester.WALRecord) error {
	return nil
}

func (n *noopWAL) Stop() error {
	return nil
}
