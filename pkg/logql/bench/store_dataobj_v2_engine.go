package bench

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore/providers/filesystem"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

var (
	engineLogs      = flag.Bool("engine-logs", false, "include engine logs in verbose output")
	remoteTransport = flag.Bool("remote-transport", false, "run engine with remote transport over loopback interface")
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
	ctx := context.Background()
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

	var (
		schedSrv  *server.Server
		workerSrv *server.Server

		schedSvc  services.Service
		workerSvc services.Service

		schedAdvertiseAddr  net.Addr
		workerAdvertiseAddr net.Addr

		schedLookupAddr string
	)

	if *remoteTransport {
		schedSrv, schedSvc, err = newServerService("scheduler", logger, prometheus.NewRegistry())
		if err != nil {
			return nil, fmt.Errorf("failed to create scheduler server: %w", err)
		} else if err := services.StartAndAwaitRunning(ctx, schedSvc); err != nil {
			return nil, fmt.Errorf("starting scheduler service: %w", err)
		}
		schedAdvertiseAddr = schedSrv.HTTPListenAddr()
	}

	sched, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:        log.With(logger, "component", "scheduler"),
		AdvertiseAddr: schedAdvertiseAddr,
	})

	if err != nil {
		return nil, fmt.Errorf("creating scheduler: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, sched.Service()); err != nil {
		return nil, fmt.Errorf("starting scheduler service: %w", err)
	}

	if *remoteTransport {
		workerSrv, workerSvc, err = newServerService("worker", logger, prometheus.NewRegistry())
		if err != nil {
			return nil, fmt.Errorf("failed to create worker server: %w", err)
		} else if err := services.StartAndAwaitRunning(ctx, workerSvc); err != nil {
			return nil, fmt.Errorf("starting worker service: %w", err)
		}
		workerAdvertiseAddr = workerSrv.HTTPListenAddr()
		schedLookupAddr = schedAdvertiseAddr.String()
	}

	worker, err := engine.NewWorker(engine.WorkerParams{
		Logger:         log.With(logger, "component", "worker"),
		AdvertiseAddr:  workerAdvertiseAddr,
		Bucket:         bucketClient,
		LocalScheduler: sched,
		Config: engine.WorkerConfig{
			SchedulerLookupAddress:  schedLookupAddr,
			SchedulerLookupInterval: 60,
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
	} else if err := services.StartAndAwaitRunning(ctx, worker.Service()); err != nil {
		return nil, fmt.Errorf("starting worker service: %w", err)
	}

	if *remoteTransport {
		sched.RegisterSchedulerServer(schedSrv.HTTP)
		worker.RegisterWorkerServer(workerSrv.HTTP)
	}

	registerer := prometheus.NewRegistry()
	ms := metastore.NewObjectMetastore(bucketClient, metastoreCfg, logger, metastore.NewObjectMetastoreMetrics(registerer))
	newEngine, err := engine.New(engine.Params{
		Logger:     logger,
		Registerer: registerer,
		Config:     cfg,
		Scheduler:  sched,
		Limits:     logql.NoLimits,
		Metastore:  ms,
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

func newServerService(name string, logger log.Logger, registerer prometheus.Registerer) (*server.Server, services.Service, error) {
	logger = log.With(logger, "component", "server", "server", name)
	serv, err := server.New(server.Config{
		Log:               logger,
		Registerer:        registerer,
		HTTPListenNetwork: "tcp",
		HTTPListenAddress: "localhost",
		HTTPListenPort:    0,
	})
	if err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	svc := services.NewBasicService(
		nil,
		func(ctx context.Context) error {
			level.Info(logger).Log("msg", "server starting up")
			go func() {
				defer close(done)
				done <- serv.Run()
			}()
			select {
			case <-ctx.Done():
				return nil
			case err := <-done:
				return err
			}
		},
		func(err error) error {
			level.Info(logger).Log("msg", "server shutting down", "err", err)
			serv.Shutdown()
			<-done
			return nil
		},
	)

	// Enable HTTP/2
	serv.HTTPServer.Handler = h2c.NewHandler(serv.HTTPServer.Handler, &http2.Server{})

	return serv, svc, nil
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
