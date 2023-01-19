package wal

import (
	"fmt"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/pkg/ingester"
)

var (
	recordPool = ingester.NewRecordPool()
)

// WAL is an interface that allows us to abstract ourselves from Prometheus WAL implementation.
type WAL interface {
	// Log marshalls the records and writes it into the WAL.
	Log(*ingester.WALRecord) error
	Delete() error
	Sync() error
	Dir() string
}

type wrapper struct {
	wal *wlog.WL
	log log.Logger
}

// New creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL. Note that the WAL created by
// New uses as directory the following path structure: cfg.Dir/clientName/tenantID.
func New(cfg Config, log log.Logger, registerer prometheus.Registerer) (WAL, error) {
	tsdbWAL, err := wlog.NewSize(log, registerer, cfg.Dir, wlog.DefaultSegmentSize, false)
	if err != nil {
		return nil, fmt.Errorf("failde to create tsdb WAL: %w", err)
	}
	return &wrapper{
		wal: tsdbWAL,
		log: log,
	}, nil
}

func (w *wrapper) Close() error {
	return w.wal.Close()
}

func (w *wrapper) Delete() error {
	err := w.wal.Close()
	if err != nil {
		level.Warn(w.log).Log("msg", "failed to close WAL", "err", err)
	}
	err = os.RemoveAll(w.wal.Dir())
	return err
}

func (w *wrapper) Log(record *ingester.WALRecord) error {
	if record == nil || (len(record.Series) == 0 && len(record.RefEntries) == 0) {
		return nil
	}

	// todo we don't new a pool this is synchronous
	buf := recordPool.GetBytes()[:0]
	defer func() {
		recordPool.PutBytes(buf)
	}()

	// Always write series then entries.
	if len(record.Series) > 0 {
		buf = record.EncodeSeries(buf)
		if err := w.wal.Log(buf); err != nil {
			return err
		}
		buf = buf[:0]
	}
	if len(record.RefEntries) > 0 {
		buf = record.EncodeEntries(ingester.CurrentEntriesRec, buf)
		if err := w.wal.Log(buf); err != nil {
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
