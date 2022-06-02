package client

import (
	"os"
	"path"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wal"
)

var (
	NoopWAL    = &noopWAL{}
	recordPool = newRecordPool()
)

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	// Log marshalls the records and writes it into the WAL.
	Log(*ingester.WALRecord) error
	Delete() error
}

type noopWAL struct{}

func (n noopWAL) Log(*ingester.WALRecord) error {
	return nil
}

func (n noopWAL) Delete() error {
	return nil
}

type walWrapper struct {
	wal *wal.WAL
	log log.Logger
}

// newWAL creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL.
func newWAL(log log.Logger, registerer prometheus.Registerer, cfg WALConfig, clientName string, tenantID string) (WAL, error) {
	if !cfg.Enabled {
		return NoopWAL, nil
	}

	dir := path.Join(cfg.Dir, clientName, tenantID)
	tsdbWAL, err := wal.NewSize(log, registerer, dir, wal.DefaultSegmentSize, false)
	if err != nil {
		return nil, err
	}
	w := &walWrapper{
		wal: tsdbWAL,
		log: log,
	}

	return w, nil
}

func (w *walWrapper) Close() error {
	return w.wal.Close()
}

func (w *walWrapper) Delete() error {
	err := w.wal.Close()
	err = os.RemoveAll(w.wal.Dir())
	return err
}

func (w *walWrapper) Log(record *ingester.WALRecord) error {
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

type resettingPool struct {
	rPool *sync.Pool // records
	ePool *sync.Pool // entries
	bPool *sync.Pool // bytes
}

func (p *resettingPool) GetRecord() *ingester.WALRecord {
	rec := p.rPool.Get().(*ingester.WALRecord)
	rec.Reset()
	return rec
}

func (p *resettingPool) PutRecord(r *ingester.WALRecord) {
	p.rPool.Put(r)
}

func (p *resettingPool) GetEntries() []logproto.Entry {
	return p.ePool.Get().([]logproto.Entry)
}

func (p *resettingPool) PutEntries(es []logproto.Entry) {
	p.ePool.Put(es[:0]) // nolint:staticcheck
}

func (p *resettingPool) GetBytes() []byte {
	return p.bPool.Get().([]byte)
}

func (p *resettingPool) PutBytes(b []byte) {
	p.bPool.Put(b[:0]) // nolint:staticcheck
}

func newRecordPool() *resettingPool {
	return &resettingPool{
		rPool: &sync.Pool{
			New: func() interface{} {
				return &ingester.WALRecord{}
			},
		},
		ePool: &sync.Pool{
			New: func() interface{} {
				return make([]logproto.Entry, 0, 512)
			},
		},
		bPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1<<10) // 1kb
			},
		},
	}
}
