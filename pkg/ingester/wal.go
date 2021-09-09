package ingester

import (
	"flag"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	// shared pool for WALRecords and []logproto.Entries
	recordPool = newRecordPool()
)

const walSegmentSize = wal.DefaultSegmentSize * 4
const defaultCeiling = 4 << 30 // 4GB

type WALConfig struct {
	Enabled             bool             `yaml:"enabled"`
	Dir                 string           `yaml:"dir"`
	CheckpointDuration  time.Duration    `yaml:"checkpoint_duration"`
	FlushOnShutdown     bool             `yaml:"flush_on_shutdown"`
	ReplayMemoryCeiling flagext.ByteSize `yaml:"replay_memory_ceiling"`
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
	f.DurationVar(&cfg.CheckpointDuration, "ingester.checkpoint-duration", 5*time.Minute, "Interval at which checkpoints should be created.")
	f.BoolVar(&cfg.FlushOnShutdown, "ingester.flush-on-shutdown", false, "When WAL is enabled, should chunks be flushed to long-term storage on shutdown.")

	// Need to set default here
	cfg.ReplayMemoryCeiling = flagext.ByteSize(defaultCeiling)
	f.Var(&cfg.ReplayMemoryCeiling, "ingester.wal-replay-memory-ceiling", "How much memory the WAL may use during replay before it needs to flush chunks to storage, i.e. 10GB. We suggest setting this to a high percentage (~75%) of available memory.")
}

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	Start()
	// Log marshalls the records and writes it into the WAL.
	Log(*WALRecord) error
	// Stop stops all the WAL operations.
	Stop() error
}

type noopWAL struct{}

func (noopWAL) Start()               {}
func (noopWAL) Log(*WALRecord) error { return nil }
func (noopWAL) Stop() error          { return nil }

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

	tsdbWAL, err := wal.NewSize(util_log.Logger, registerer, cfg.Dir, walSegmentSize, false)
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

	return w, nil
}

func (w *walWrapper) Start() {
	w.wait.Add(1)
	go w.run()
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
			buf = record.encodeEntries(CurrentEntriesRec, buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.metrics.walRecordsLogged.Inc()
			w.metrics.walLoggedBytesTotal.Add(float64(len(buf)))
		}
		return nil
	}
}

func (w *walWrapper) Stop() error {
	close(w.quit)
	w.wait.Wait()
	err := w.wal.Close()
	level.Info(util_log.Logger).Log("msg", "stopped", "component", "wal")
	return err
}

func (w *walWrapper) checkpointWriter() *WALCheckpointWriter {
	return &WALCheckpointWriter{
		metrics:    w.metrics,
		segmentWAL: w.wal,
	}
}

func (w *walWrapper) run() {
	level.Info(util_log.Logger).Log("msg", "started", "component", "wal")
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
