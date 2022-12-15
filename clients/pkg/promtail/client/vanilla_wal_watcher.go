package client

import (
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/pkg/ingester"
)

type WALReader interface {
	Next() bool
	Err() error
	// Record should not be used across multiple calls to Next()
	Record() []byte
}

type WALConsumer interface {
	NumWorkers() int
	SetStream(tenantID string, series record.RefSeries) error
	Push(tenantID string, entries ingester.RefEntries) error
	Done() <-chan struct{}
}

type VanillaWALWatcher struct {
	consumer WALConsumer
}

func NewVanillaWALWatcher(consumer WALConsumer) *VanillaWALWatcher {
	return &VanillaWALWatcher{consumer}
}

func (w *VanillaWALWatcher) Watch(reader WALReader) error {
	for reader.Next() {
		b := reader.Record()
		if err := reader.Err(); err != nil {
			return err
		}

		if err := w.dispatch(b); err != nil {
			return err
		}
	}
	return nil
}

func (w *VanillaWALWatcher) dispatch(b []byte) error {
	rec := recordPool.GetRecord()
	if err := ingester.DecodeWALRecord(b, rec); err != nil {
		return err
	}

	// First process all series to ensure we don't write entries to nonexistant series.
	var firstErr error
	for _, s := range rec.Series {
		if err := w.consumer.SetStream(rec.UserID, s); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}

	}

	for _, entries := range rec.RefEntries {
		if err := w.consumer.Push(rec.UserID, entries); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr

}
