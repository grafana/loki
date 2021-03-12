package flusher

import (
	"context"
	"flag"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ingester"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// Config for an Ingester.
type Config struct {
	WALDir            string        `yaml:"wal_dir"`
	ConcurrentFlushes int           `yaml:"concurrent_flushes"`
	FlushOpTimeout    time.Duration `yaml:"flush_op_timeout"`
	ExitAfterFlush    bool          `yaml:"exit_after_flush"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WALDir, "flusher.wal-dir", "wal", "Directory to read WAL from (chunks storage engine only).")
	f.IntVar(&cfg.ConcurrentFlushes, "flusher.concurrent-flushes", 50, "Number of concurrent goroutines flushing to storage (chunks storage engine only).")
	f.DurationVar(&cfg.FlushOpTimeout, "flusher.flush-op-timeout", 2*time.Minute, "Timeout for individual flush operations (chunks storage engine only).")
	f.BoolVar(&cfg.ExitAfterFlush, "flusher.exit-after-flush", true, "Stop Cortex after flush has finished. If false, Cortex process will keep running, doing nothing.")
}

// Flusher is designed to be used as a job to flush the data from the WAL on disk.
// Flusher works with both chunks-based and blocks-based ingesters.
type Flusher struct {
	services.Service

	cfg            Config
	ingesterConfig ingester.Config
	chunkStore     ingester.ChunkStore
	limits         *validation.Overrides
	registerer     prometheus.Registerer
	logger         log.Logger
}

const (
	postFlushSleepTime = 1 * time.Minute
)

// New constructs a new Flusher and flushes the data from the WAL.
// The returned Flusher has no other operations.
func New(
	cfg Config,
	ingesterConfig ingester.Config,
	chunkStore ingester.ChunkStore,
	limits *validation.Overrides,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*Flusher, error) {

	// These are ignored by blocks-ingester, but that's fine.
	ingesterConfig.WALConfig.Dir = cfg.WALDir
	ingesterConfig.ConcurrentFlushes = cfg.ConcurrentFlushes
	ingesterConfig.FlushOpTimeout = cfg.FlushOpTimeout

	f := &Flusher{
		cfg:            cfg,
		ingesterConfig: ingesterConfig,
		chunkStore:     chunkStore,
		limits:         limits,
		registerer:     registerer,
		logger:         logger,
	}
	f.Service = services.NewBasicService(nil, f.running, nil)
	return f, nil
}

func (f *Flusher) running(ctx context.Context) error {
	ing, err := ingester.NewForFlusher(f.ingesterConfig, f.chunkStore, f.limits, f.registerer, f.logger)
	if err != nil {
		return errors.Wrap(err, "create ingester")
	}

	if err := services.StartAndAwaitRunning(ctx, ing); err != nil {
		return errors.Wrap(err, "start and await running ingester")
	}

	ing.Flush()

	// Sleeping to give a chance to Prometheus
	// to collect the metrics.
	level.Info(f.logger).Log("msg", "sleeping to give chance for collection of metrics", "duration", postFlushSleepTime.String())
	time.Sleep(postFlushSleepTime)

	if err := services.StopAndAwaitTerminated(ctx, ing); err != nil {
		return errors.Wrap(err, "stop and await terminated ingester")
	}

	if f.cfg.ExitAfterFlush {
		return util.ErrStopProcess
	}

	// Return normally -- this keep Cortex running.
	return nil
}
