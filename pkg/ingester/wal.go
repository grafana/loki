package ingester

import (
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	// shared pool for WALRecords and []logproto.Entries
	recordPool = wal.NewRecordPool()
)

const walSegmentSize = wlog.DefaultSegmentSize * 4
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
		return fmt.Errorf("invalid checkpoint duration: %v", cfg.CheckpointDuration)
	}
	return nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *WALConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Dir, "ingester.wal-dir", "wal", "Directory where the WAL data is stored and/or recovered from.")
	f.BoolVar(&cfg.Enabled, "ingester.wal-enabled", true, "Enable writing of ingested data into WAL.")
	f.DurationVar(&cfg.CheckpointDuration, "ingester.checkpoint-duration", 5*time.Minute, "Interval at which checkpoints should be created.")
	f.BoolVar(&cfg.FlushOnShutdown, "ingester.flush-on-shutdown", false, "When WAL is enabled, should chunks be flushed to long-term storage on shutdown.")

	// Need to set default here
	cfg.ReplayMemoryCeiling = flagext.ByteSize(defaultCeiling)
	f.Var(&cfg.ReplayMemoryCeiling, "ingester.wal-replay-memory-ceiling", "Maximum memory size the WAL may use during replay. After hitting this, it will flush data to storage before continuing. A unit suffix (KB, MB, GB) may be applied.")
}

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	Start()
	// Log marshalls the records and writes it into the WAL.
	Log(*wal.Record) error
	// Stop stops all the WAL operations.
	Stop() error
}

type noopWAL struct{}

func (noopWAL) Start()                {}
func (noopWAL) Log(*wal.Record) error { return nil }
func (noopWAL) Stop() error           { return nil }

type walWrapper struct {
	cfg        WALConfig
	wal        *wlog.WL
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

	tsdbWAL, err := wlog.NewSize(util_log.Logger, registerer, cfg.Dir, walSegmentSize, wlog.CompressionNone)
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

func (w *walWrapper) Log(record *wal.Record) error {
	if record == nil || (len(record.Series) == 0 && len(record.RefEntries) == 0) {
		return nil
	}
	select {
	case <-w.quit:
		return errors.New("wal is stopped")
	default:
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
			w.metrics.walRecordsLogged.Inc()
			w.metrics.walLoggedBytesTotal.Add(float64(len(*buf)))
			*buf = (*buf)[:0]
		}
		if len(record.RefEntries) > 0 {
			*buf = record.EncodeEntries(wal.CurrentEntriesRec, *buf)
			if err := w.wal.Log(*buf); err != nil {
				return err
			}
			w.metrics.walRecordsLogged.Inc()
			w.metrics.walLoggedBytesTotal.Add(float64(len(*buf)))
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
