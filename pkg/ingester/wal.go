package ingester

import (
	"flag"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/grafana/loki/pkg/logproto"
)

/*
need to rebuild
- user states
  - inverted index
  - map[fp]stream


plan without checkpoint:
1) Read wal records, mapping FPs & creating series into map[userId]map[uint64]stream
2) ensure fpmapper & inverted index are created/used.
3) iterate wal samples? Do this after b/c chunks may be cut while adding wal samples.

After recovering from a checkpoint, all flushed chunks that are now passed the retention config may be dropped

Error conditions to test:
- stream limited by limiter should not apply during wal replay. Can be hacked by setting an unlimited limiter then overriding after wal replay.
- corrupted segments/records?

Keep in mind:
- should we keep memory ballast to prevent alloc-ooming during checkpointing?
- helpful operator errors for remediation actions
*/

var (
	// shared pool for WALRecords and []logproto.Entries
	recordPool = newRecordPool()
)

type WALConfig struct {
	Enabled            bool          `yaml:"enabled"`
	Dir                string        `yaml:"dir"`
	Recover            bool          `yaml:"recover"`
	CheckpointDuration time.Duration `yaml:"checkpoint_duration"`
}

func (cfg *WALConfig) Validate() error {
	if cfg.Enabled && cfg.CheckpointDuration < 1 {
		return errors.Errorf("invalid checkpoint duration: %v", cfg.CheckpointDuration)
	}
	return nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *WALConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Dir, "ingester.wal-dir", "wal", "Directory to store the WAL and/or recover from WAL.")
	f.BoolVar(&cfg.Enabled, "ingester.wal-enabled", false, "Enable writing of ingested data into WAL.")
	f.BoolVar(&cfg.Recover, "ingester.recover-from-wal", false, "Recover data from existing WAL irrespective of WAL enabled/disabled.")
	f.DurationVar(&cfg.CheckpointDuration, "ingester.checkpoint-duration", 15*time.Minute, "Interval at which checkpoints should be created.")
}

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	// Log marshalls the records and writes it into the WAL.
	Log(*WALRecord) error
	// Stop stops all the WAL operations.
	Stop()
}

type noopWAL struct{}

func (noopWAL) Log(*WALRecord) error { return nil }
func (noopWAL) Stop()                {}

type walWrapper struct {
	cfg        WALConfig
	wal        *wal.WAL
	metrics    *ingesterMetrics
	seriesIter SeriesIter

	wait sync.WaitGroup
	quit chan struct{}
}

// newWAL creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL.
func newWAL(cfg WALConfig, registerer prometheus.Registerer, metrics *ingesterMetrics, seriesIter SeriesIter) (WAL, error) {
	if !cfg.Enabled {
		return noopWAL{}, nil
	}

	tsdbWAL, err := wal.NewSize(util.Logger, registerer, cfg.Dir, wal.DefaultSegmentSize*4, false)
	if err != nil {
		return nil, err
	}

	w := &walWrapper{
		cfg:        cfg,
		quit:       make(chan struct{}),
		wal:        tsdbWAL,
		metrics:    metrics,
		seriesIter: seriesIter,
	}

	w.wait.Add(1)
	go w.run()
	return w, nil
}

func (w *walWrapper) Log(record *WALRecord) error {
	if record == nil || (len(record.Series) == 0 && len(record.RefEntries) == 0) {
		return nil
	}
	select {
	case <-w.quit:
		return nil
	default:
		buf := recordPool.GetBytes()[:0]
		defer func() {
			recordPool.PutBytes(buf)
		}()

		// Always write series then entries.
		if len(record.Series) > 0 {
			buf = record.encodeSeries(buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.metrics.walRecordsLogged.Inc()
			w.metrics.walLoggedBytesTotal.Add(float64(len(buf)))
			buf = buf[:0]
		}
		if len(record.RefEntries) > 0 {
			buf = record.encodeEntries(buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.metrics.walRecordsLogged.Inc()
			w.metrics.walLoggedBytesTotal.Add(float64(len(buf)))
		}
		return nil
	}
}

func (w *walWrapper) Stop() {
	close(w.quit)
	w.wait.Wait()
	_ = w.wal.Close()
	level.Info(util.Logger).Log("msg", "stopped", "component", "wal")
}

func (w *walWrapper) checkpointWriter() *WALCheckpointWriter {
	return &WALCheckpointWriter{
		metrics:    w.metrics,
		segmentWAL: w.wal,
	}
}

func (w *walWrapper) run() {
	level.Info(util.Logger).Log("msg", "started", "component", "wal")
	defer w.wait.Done()

	checkpointer := NewCheckpointer(
		w.cfg.CheckpointDuration,
		w.seriesIter,
		w.checkpointWriter(),
		w.metrics,
		w.quit,
	)
	checkpointer.Run()

}

type resettingPool struct {
	rPool *sync.Pool // records
	ePool *sync.Pool // entries
	bPool *sync.Pool // bytes
}

func (p *resettingPool) GetRecord() *WALRecord {
	rec := p.rPool.Get().(*WALRecord)
	rec.Reset()
	return rec
}

func (p *resettingPool) PutRecord(r *WALRecord) {
	p.rPool.Put(r)
}

func (p *resettingPool) GetEntries() []logproto.Entry {
	return p.ePool.Get().([]logproto.Entry)
}

func (p *resettingPool) PutEntries(es []logproto.Entry) {
	p.ePool.Put(es[:0])
}

func (p *resettingPool) GetBytes() []byte {
	return p.bPool.Get().([]byte)
}

func (p *resettingPool) PutBytes(b []byte) {
	p.bPool.Put(b[:0])
}

func newRecordPool() *resettingPool {
	return &resettingPool{
		rPool: &sync.Pool{
			New: func() interface{} {
				return &WALRecord{}
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
