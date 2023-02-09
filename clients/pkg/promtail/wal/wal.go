package wal

import (
	"fmt"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/pkg/ingester/wal"
)

var (
	recordPool = wal.NewRecordPool()
)

// WAL is an interface that allows us to abstract ourselves from Prometheus WAL implementation.
type WAL interface {
	// Log marshals the records and writes it into the WAL.
	Log(*wal.Record) error

	Delete() error
	Sync() error
	Dir() string
	Close()
	NextSegment() (int, error)
}

type wrapper struct {
	wal *wlog.WL
	log log.Logger
}

// New creates a new wrapper, instantiating the actual wlog.WL underneath.
func New(cfg Config, log log.Logger, registerer prometheus.Registerer) (WAL, error) {
	// TODO: We should fine-tune the WAL instantiated here to allow some buffering of written entries, but not written to disk
	// yet. This will attest for the lack of buffering in the channel Writer exposes.
	tsdbWAL, err := wlog.NewSize(log, registerer, cfg.Dir, wlog.DefaultSegmentSize, false)
	if err != nil {
		return nil, fmt.Errorf("failde to create tsdb WAL: %w", err)
	}
	return &wrapper{
		wal: tsdbWAL,
		log: log,
	}, nil
}

// Close closes the underlying wal, flushing pending writes and closing the active segment. Safe to call more than once
func (w *wrapper) Close() {
	// Avoid checking the error since it's safe to call Close more than once on wlog.WL
	_ = w.wal.Close()
}

func (w *wrapper) Delete() error {
	err := w.wal.Close()
	if err != nil {
		level.Warn(w.log).Log("msg", "failed to close WAL", "err", err)
	}
	err = os.RemoveAll(w.wal.Dir())
	return err
}

// Log writes a new wal.Record to the WAL. It's safe to assume that a record always contains at least one entry
// and one series.
func (w *wrapper) Log(record *wal.Record) error {
	if record == nil || (len(record.Series) == 0 && len(record.RefEntries) == 0) {
		return nil
	}
	// After here we know that there's a record, and it contains either entries or series, but knowing the caller,
	// we can assume it always does one of both for batching both entries and series records.

	seriesBuf := recordPool.GetBytes()[:0]
	entriesBuf := recordPool.GetBytes()[:0]
	defer func() {
		recordPool.PutBytes(seriesBuf)
		recordPool.PutBytes(entriesBuf)
	}()

	seriesBuf = record.EncodeSeries(seriesBuf)
	entriesBuf = record.EncodeEntries(wal.CurrentEntriesRec, entriesBuf)
	// Batch both wal records written to just produce one page flush after. Always write series then entries.
	if err := w.wal.Log(seriesBuf, entriesBuf); err != nil {
		return err
	}
	return nil
}

// Sync flushes changes to disk. Mainly to be used for testing.
func (w *wrapper) Sync() error {
	return w.wal.Sync()
}

// Dir returns the path to the WAL directory.
func (w *wrapper) Dir() string {
	return w.wal.Dir()
}

// NextSegment closes the current segment synchronously. Mainly used for testing.
func (w *wrapper) NextSegment() (int, error) {
	return w.wal.NextSegmentSync()
}
