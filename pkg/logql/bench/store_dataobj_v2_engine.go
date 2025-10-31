package bench

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

var errStoreUnimplemented = errors.New("store does not implement this operation")

var (
	engineLogs = flag.Bool("engine-logs", false, "include engine logs in verbose output")
)

// DataObjV2EngineStore uses the new engine for querying. It assumes the engine
// can read the "new dataobj format" (e.g. columnar data) from the provided
// dataDir via a filesystem objstore.Bucket.
type DataObjV2EngineStore struct {
	engine   logql.Engine // Use the interface type
	tenantID string
	dataDir  string
}

// NewDataObjV2EngineStore creates a new store that uses the v2 dataobj engine.
func NewDataObjV2EngineStore(dir string, tenantID string) (*DataObjV2EngineStore, error) {
	storageDir := filepath.Join(dir, storageDir)
	return dataobjV2StoreWithOpts(storageDir, tenantID, engine.ExecutorConfig{
		BatchSize:          512,
		RangeConfig:        rangeio.DefaultConfig,
		MergePrefetchCount: 8,
	}, metastore.Config{
		IndexStoragePrefix: "index/v0",
	})
}

func dataobjV2StoreWithOpts(dataDir string, tenantID string, cfg engine.ExecutorConfig, metastoreCfg metastore.Config) (*DataObjV2EngineStore, error) {
	logger := log.NewNopLogger()

	if testing.Verbose() && *engineLogs {
		startTime := time.Now()

		logger = log.NewLogfmtLogger(&unescapeWriter{w: log.NewSyncWriter(os.Stderr)})

		// Rather than reporting timestamp, it can be useful in the context of
		// our tests and benchmarks to report the elapsed time since the store
		// was created.
		logger = log.With(logger, "elapsed", log.Valuer(func() any {
			return time.Since(startTime).String()
		}))
	}

	storeDir := filepath.Join(dataDir, "dataobj")
	bucketClient, err := filesystem.NewBucket(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem bucket for DataObjV2EngineStore: %w", err)
	}

	sched, err := engine.NewScheduler(logger)
	if err != nil {
		return nil, fmt.Errorf("creating scheduler: %w", err)
	} else if err := services.StartAndAwaitRunning(context.Background(), sched.Service()); err != nil {
		return nil, fmt.Errorf("starting scheduler service: %w", err)
	}

	worker, err := engine.NewWorker(engine.WorkerParams{
		Logger:         logger,
		Bucket:         bucketClient,
		LocalScheduler: sched,
		Config: engine.WorkerConfig{
			// Try to create one thread per host CPU core. However, we always
			// create at least 8 threads. This prevents situations where
			// no task can make progress because a parent task hasn't been
			// scheduled yet.
			//
			// Eventually, this will be fixed by the scheduler detecting
			// deadlocks and preempting deadlocked tasks. In the meantime, 8
			// threads is always more than enough for any currently producible
			// LogQL query.
			WorkerThreads: max(runtime.GOMAXPROCS(0), 8),
		},
		Executor: cfg,
	})
	if err != nil {
		return nil, fmt.Errorf("creating worker: %w", err)
	} else if err := services.StartAndAwaitRunning(context.Background(), worker.Service()); err != nil {
		return nil, fmt.Errorf("starting worker service: %w", err)
	}

	newEngine, err := engine.New(engine.Params{
		Logger:          logger,
		Registerer:      prometheus.NewRegistry(),
		Config:          cfg,
		MetastoreConfig: metastoreCfg,
		Scheduler:       sched,
		Bucket:          bucketClient,
		Limits:          logql.NoLimits,
	})
	if err != nil {
		return nil, fmt.Errorf("creating engine: %w", err)
	}

	return &DataObjV2EngineStore{
		engine:   (*engineAdapter)(newEngine),
		tenantID: tenantID, // Store for context or if querier needs it
		dataDir:  dataDir,
	}, nil
}

type engineAdapter engine.Engine

var _ logql.Engine = (*engineAdapter)(nil)

func (a *engineAdapter) Query(p logql.Params) logql.Query {
	return &queryAdapter{engine: (*engine.Engine)(a), params: p}
}

type queryAdapter struct {
	engine *engine.Engine
	params logql.Params
}

func (a *queryAdapter) Exec(ctx context.Context) (logqlmodel.Result, error) {
	return a.engine.Execute(ctx, a.params)
}

// unescapeWriter is a writer that unescapes newline characters in messages with
// actual newlines.
//
// This is useful for making it easier to read engine plans, which have text
// across multiple lines.
type unescapeWriter struct{ w io.Writer }

func (uw *unescapeWriter) Write(p []byte) (int, error) {
	if bytes.Count(p, []byte("\\n")) == 0 {
		// Write without replacement.
		return uw.w.Write(p)
	}

	replaced := bytes.ReplaceAll(p, []byte("\\n"), []byte("\n\t"))
	n, err := uw.w.Write(replaced)
	if err != nil {
		return 0, err
	} else if n != len(replaced) {
		return 0, io.ErrShortWrite
	}

	return len(p), nil
}
