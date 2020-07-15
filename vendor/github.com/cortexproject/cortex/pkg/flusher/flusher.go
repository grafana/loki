package flusher

import (
	"context"
	"flag"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ingester"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// Config for an Ingester.
type Config struct {
	WALDir            string        `yaml:"wal_dir"`
	ConcurrentFlushes int           `yaml:"concurrent_flushes"`
	FlushOpTimeout    time.Duration `yaml:"flush_op_timeout"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WALDir, "flusher.wal-dir", "wal", "Directory to read WAL from.")
	f.IntVar(&cfg.ConcurrentFlushes, "flusher.concurrent-flushes", 50, "Number of concurrent goroutines flushing to dynamodb.")
	f.DurationVar(&cfg.FlushOpTimeout, "flusher.flush-op-timeout", 2*time.Minute, "Timeout for individual flush operations.")
}

// Flusher is designed to be used as a job to flush the chunks from the WAL on disk.
type Flusher struct {
	services.Service

	cfg            Config
	ingesterConfig ingester.Config
	clientConfig   client.Config
	chunkStore     ingester.ChunkStore
	registerer     prometheus.Registerer
}

const (
	postFlushSleepTime = 1 * time.Minute
)

// New constructs a new Flusher and flushes the data from the WAL.
// The returned Flusher has no other operations.
func New(
	cfg Config,
	ingesterConfig ingester.Config,
	clientConfig client.Config,
	chunkStore ingester.ChunkStore,
	registerer prometheus.Registerer,
) (*Flusher, error) {

	ingesterConfig.WALConfig.Dir = cfg.WALDir
	ingesterConfig.ConcurrentFlushes = cfg.ConcurrentFlushes
	ingesterConfig.FlushOpTimeout = cfg.FlushOpTimeout

	f := &Flusher{
		cfg:            cfg,
		ingesterConfig: ingesterConfig,
		clientConfig:   clientConfig,
		chunkStore:     chunkStore,
		registerer:     registerer,
	}
	f.Service = services.NewBasicService(nil, f.running, nil)
	return f, nil
}

func (f *Flusher) running(ctx context.Context) error {
	ing, err := ingester.NewForFlusher(f.ingesterConfig, f.clientConfig, f.chunkStore, f.registerer)
	if err != nil {
		return errors.Wrap(err, "create ingester")
	}

	if err := services.StartAndAwaitRunning(ctx, ing); err != nil {
		return errors.Wrap(err, "start and await running ingester")
	}

	ing.Flush()

	// Sleeping to give a chance to Prometheus
	// to collect the metrics.
	level.Info(util.Logger).Log("msg", "sleeping to give chance for collection of metrics", "duration", postFlushSleepTime.String())
	time.Sleep(postFlushSleepTime)

	if err := services.StopAndAwaitTerminated(ctx, ing); err != nil {
		return errors.Wrap(err, "stop and await terminated ingester")
	}
	return util.ErrStopProcess
}
