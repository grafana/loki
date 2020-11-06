package ingester

import (
	"flag"
	"sync"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
	Recover bool   `yaml:"recover"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *WALConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Dir, "ingester.wal-dir", "wal", "Directory to store the WAL and/or recover from WAL.")
	f.BoolVar(&cfg.Enabled, "ingester.wal-enabled", false, "Enable writing of ingested data into WAL.")
	f.BoolVar(&cfg.Recover, "ingester.recover-from-wal", false, "Recover data from existing WAL irrespective of WAL enabled/disabled.")
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
	cfg       WALConfig
	wal       *wal.WAL
	bytesPool sync.Pool

	wait sync.WaitGroup
	quit chan struct{}

	// Metrics.
	checkpointDeleteFail       prometheus.Counter
	checkpointDeleteTotal      prometheus.Counter
	checkpointCreationFail     prometheus.Counter
	checkpointCreationTotal    prometheus.Counter
	checkpointDuration         prometheus.Summary
	checkpointLoggedBytesTotal prometheus.Counter
	walLoggedBytesTotal        prometheus.Counter
	walRecordsLogged           prometheus.Counter
}

// newWAL creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL.
func newWAL(cfg WALConfig, registerer prometheus.Registerer) (WAL, error) {
	if !cfg.Enabled {
		return noopWAL{}, nil
	}

	var walRegistry prometheus.Registerer
	if registerer != nil {
		walRegistry = prometheus.WrapRegistererWith(prometheus.Labels{"kind": "wal"}, registerer)
	}

	tsdbWAL, err := wal.NewSize(util.Logger, walRegistry, cfg.Dir, wal.DefaultSegmentSize*4, false)
	if err != nil {
		return nil, err
	}

	w := &walWrapper{
		cfg:  cfg,
		quit: make(chan struct{}),
		wal:  tsdbWAL,
		bytesPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1<<10) // 1kb
			},
		},
	}

	w.checkpointDeleteFail = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_checkpoint_deletions_failed_total",
		Help: "Total number of checkpoint deletions that failed.",
	})
	w.checkpointDeleteTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_checkpoint_deletions_total",
		Help: "Total number of checkpoint deletions attempted.",
	})
	w.checkpointCreationFail = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_checkpoint_creations_failed_total",
		Help: "Total number of checkpoint creations that failed.",
	})
	w.checkpointCreationTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_checkpoint_creations_total",
		Help: "Total number of checkpoint creations attempted.",
	})
	w.checkpointDuration = promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
		Name:       "loki_ingester_checkpoint_duration_seconds",
		Help:       "Time taken to create a checkpoint.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
	w.walRecordsLogged = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_wal_records_logged_total",
		Help: "Total number of WAL records logged.",
	})
	w.checkpointLoggedBytesTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_checkpoint_logged_bytes_total",
		Help: "Total number of bytes written to disk for checkpointing.",
	})
	w.walLoggedBytesTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_wal_logged_bytes_total",
		Help: "Total number of bytes written to disk for WAL records.",
	})

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
		buf := w.bytesPool.Get().([]byte)[:0]
		defer func() {
			w.bytesPool.Put(buf) // nolint:staticcheck
		}()

		// Always write series then entries.
		if len(record.Series) > 0 {
			buf = record.encodeSeries(buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.walRecordsLogged.Inc()
			w.walLoggedBytesTotal.Add(float64(len(buf)))
			buf = buf[:0]
		}
		if len(record.RefEntries) > 0 {
			buf = record.encodeEntries(buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.walRecordsLogged.Inc()
			w.walLoggedBytesTotal.Add(float64(len(buf)))
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

func (w *walWrapper) run() {
	level.Info(util.Logger).Log("msg", "started", "component", "wal")
	defer w.wait.Done()
}

type resettingPool struct {
	rPool *sync.Pool
	ePool *sync.Pool
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
	}
}
