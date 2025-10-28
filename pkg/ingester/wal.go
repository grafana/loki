package ingester

import (
	"flag"
	"fmt"
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wlog"
	"github.com/prometheus/prometheus/util/compression"

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
	DiskFullThreshold   float64          `yaml:"disk_full_threshold"`
}

func (cfg *WALConfig) Validate() error {
	if cfg.Enabled && cfg.CheckpointDuration < 1 {
		return fmt.Errorf("invalid checkpoint duration: %v", cfg.CheckpointDuration)
	}
	if cfg.DiskFullThreshold < 0 || cfg.DiskFullThreshold > 1 {
		return fmt.Errorf("invalid disk full threshold: %v (must be between 0 and 1)", cfg.DiskFullThreshold)
	}
	return nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *WALConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Dir, "ingester.wal-dir", "wal", "Directory where the WAL data is stored and/or recovered from.")
	f.BoolVar(&cfg.Enabled, "ingester.wal-enabled", true, "Enable writing of ingested data into WAL.")
	f.DurationVar(&cfg.CheckpointDuration, "ingester.checkpoint-duration", 5*time.Minute, "Interval at which checkpoints should be created.")
	f.BoolVar(&cfg.FlushOnShutdown, "ingester.flush-on-shutdown", false, "When WAL is enabled, should chunks be flushed to long-term storage on shutdown.")
	f.Float64Var(&cfg.DiskFullThreshold, "ingester.wal-disk-full-threshold", 0.90, "Threshold for disk usage (0.0 to 1.0) at which the WAL will throttle incoming writes. Set to 0 to disable throttling.")

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
	// IsDiskThrottled returns true if the disk is too full and writes should be throttled.
	IsDiskThrottled() bool
}

type noopWAL struct{}

func (noopWAL) Start()                {}
func (noopWAL) Log(*wal.Record) error { return nil }
func (noopWAL) Stop() error           { return nil }
func (noopWAL) IsDiskThrottled() bool { return false }

type walWrapper struct {
	cfg        WALConfig
	wal        *wlog.WL
	metrics    *ingesterMetrics
	seriesIter SeriesIter

	wait          sync.WaitGroup
	quit          chan struct{}
	diskThrottled atomic.Bool
}

// newWAL creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL.
func newWAL(cfg WALConfig, registerer prometheus.Registerer, metrics *ingesterMetrics, seriesIter SeriesIter) (WAL, error) {
	if !cfg.Enabled {
		return noopWAL{}, nil
	}

	tsdbWAL, err := wlog.NewSize(util_log.SlogFromGoKit(util_log.Logger), registerer, cfg.Dir, walSegmentSize, compression.None)
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

func (w *walWrapper) IsDiskThrottled() bool {
	return w.diskThrottled.Load()
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

	// Start disk monitoring if throttling is enabled
	if w.cfg.DiskFullThreshold > 0 {
		w.wait.Add(1)
		go w.monitorDisk()
	}

	checkpointer := NewCheckpointer(
		w.cfg.CheckpointDuration,
		w.seriesIter,
		w.checkpointWriter(),
		w.metrics,
		w.quit,
	)
	checkpointer.Run()

}

// monitorDisk periodically checks disk usage and sets the throttle flag
func (w *walWrapper) monitorDisk() {
	defer w.wait.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			usage, err := w.checkDiskUsage()
			if err != nil {
				level.Warn(util_log.Logger).Log("msg", "failed to check disk usage", "err", err, "component", "wal")
				continue
			}

			wasThrottled := w.diskThrottled.Load()
			isThrottled := usage >= w.cfg.DiskFullThreshold

			if isThrottled != wasThrottled {
				w.diskThrottled.Store(isThrottled)
				if isThrottled {
					level.Warn(util_log.Logger).Log(
						"msg", "disk usage exceeded threshold, throttling writes",
						"usage_percent", fmt.Sprintf("%.2f%%", usage*100),
						"threshold_percent", fmt.Sprintf("%.2f%%", w.cfg.DiskFullThreshold*100),
						"component", "wal",
					)
					w.metrics.walDiskFullFailures.Inc()
				} else {
					level.Info(util_log.Logger).Log(
						"msg", "disk usage below threshold, resuming writes",
						"usage_percent", fmt.Sprintf("%.2f%%", usage*100),
						"threshold_percent", fmt.Sprintf("%.2f%%", w.cfg.DiskFullThreshold*100),
						"component", "wal",
					)
				}
			}

			// Update metrics with current disk usage
			w.metrics.walDiskUsagePercent.Set(usage)

		case <-w.quit:
			return
		}
	}
}
