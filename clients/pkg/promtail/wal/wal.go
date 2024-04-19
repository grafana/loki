package wal

import (
	"fmt"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
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
	tsdbWAL, err := wlog.NewSize(log, registerer, cfg.Dir, wlog.DefaultSegmentSize, wlog.CompressionNone)
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

func (w *wrapper) Log(record *wal.Record) error {
	if record == nil || (len(record.Series) == 0 && len(record.RefEntries) == 0) {
		return nil
	}

	// The code below extracts the wal write operations to when possible, batch both series and records writes
	if len(record.Series) > 0 && len(record.RefEntries) > 0 {
		return w.logBatched(record)
	}
	return w.logSingle(record)
}

// logBatched logs to the WAL both series and records, batching the operation to prevent unnecessary page flushes.
func (w *wrapper) logBatched(record *wal.Record) error {
	seriesBuf := recordPool.GetBytes()
	entriesBuf := recordPool.GetBytes()
	defer func() {
		recordPool.PutBytes(seriesBuf)
		recordPool.PutBytes(entriesBuf)
	}()

	*seriesBuf = record.EncodeSeries(*seriesBuf)
	*entriesBuf = record.EncodeEntries(wal.CurrentEntriesRec, *entriesBuf)
	// Always write series then entries
	return w.wal.Log(*seriesBuf, *entriesBuf)
}

// logSingle logs to the WAL series and records in separate WAL operation. This causes a page flush after each operation.
func (w *wrapper) logSingle(record *wal.Record) error {
	buf := recordPool.GetBytes()
	defer func() {
		recordPool.PutBytes(buf)
	}()

	// Always write series then entries.
	if len(record.Series) > 0 {
		*buf = record.EncodeSeries(*buf)
		if err := w.wal.Log(*buf); err != nil {
			return err
		}
		*buf = (*buf)[:0]
	}
	if len(record.RefEntries) > 0 {
		*buf = record.EncodeEntries(wal.CurrentEntriesRec, *buf)
		if err := w.wal.Log(*buf); err != nil {
			return err
		}

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
