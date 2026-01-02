package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kingpin/v2"
	glog "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/validation"
)

// addExecuteCommand adds the execute command to the application
func addExecuteCommand(app *kingpin.Application) {
	var cfg Config
	var engineVersion int

	cmd := app.Command("execute", "Execute query locally using the specified Loki engine but remote storage bucket")
	cmd.Flag("bucket", "Remote bucket name").Required().StringVar(&cfg.Bucket)
	cmd.Flag("org-id", "Organization ID").Required().StringVar(&cfg.OrgID)
	cmd.Flag("index-storage-prefix", "Index storage prefix for index files stored in object storage").StringVar(&indexStoragePrefix)
	cmd.Flag("start", "Start time (RFC3339 format)").Required().StringVar(&cfg.Start)
	cmd.Flag("end", "End time (RFC3339 format)").Required().StringVar(&cfg.End)
	cmd.Flag("query", "LogQL query to execute").Required().StringVar(&cfg.Query)
	cmd.Flag("limit", "Maximum number of entries to return").Default("100").IntVar(&cfg.Limit)
	cmd.Flag("engine", "Engine version (1 or 2)").Default("2").IntVar(&engineVersion)

	cmd.Action(func(_ *kingpin.ParseContext) error {
		storageBucket = cfg.Bucket
		orgID = cfg.OrgID

		parsed, err := parseTimeConfig(&cfg)
		if err != nil {
			return err
		}

		if cfg.Limit == 0 {
			cfg.Limit = 100
		}
		params, err := logql.NewLiteralParams(cfg.Query, parsed.StartTime, parsed.EndTime, 0, 0, logproto.BACKWARD, uint32(cfg.Limit), nil, nil)
		if err != nil {
			return err
		}

		switch engineVersion {
		case 1:
			return doExecuteLocallyV1(params)
		case 2:
			return doExecuteLocallyV2(params)
		default:
			return fmt.Errorf("unsupported engine version: %d (must be 1 or 2)", engineVersion)
		}
	})
}

// doExecuteLocallyV1 executes a query using the V1 engine
func doExecuteLocallyV1(params logql.LiteralParams) error {
	if indexStoragePrefix == "" {
		level.Warn(logger).Log("msg", "index storage prefix is not set. v1 engine may not find any chunks.")
	}
	level.Info(logger).Log("msg", "executing local query with V1 engine")
	result, err := doLocalQueryWithV1Engine(params)
	if err != nil {
		level.Error(logger).Log("msg", "local query with V1 engine failed", "error", err)
		return fmt.Errorf("V1 query execution failed: %w", err)
	}
	return checkResult(result)
}

// doExecuteLocallyV2 executes a query using the V2 engine
func doExecuteLocallyV2(params logql.LiteralParams) error {
	level.Info(logger).Log("msg", "executing local query with V2 engine")
	result, err := doLocalQueryWithV2Engine(params)
	if err != nil {
		level.Error(logger).Log("msg", "V2 query execution failed", "error", err)
		return fmt.Errorf("V2 query execution failed: %w", err)
	}
	return checkResult(result)
}

func doExecuteLocallyV2Scheduler(params logql.LiteralParams) error {
	level.Info(logger).Log("msg", "executing local query with V2 engine via local scheduler and worker")
	result, err := doLocalQueryWithV2EngineScheduler(params)
	if err != nil {
		level.Error(logger).Log("msg", "v2 query execution failed", "error", err)
		return fmt.Errorf("v2 query execution failed: %w", err)
	}
	return checkResult(result)
}

func doExecuteLocallyV2SchedulerRemote(params logql.LiteralParams) error {
	level.Info(logger).Log("msg", "executing local query with V2 engine via remote scheduler and worker")
	result, err := doLocalQueryWithV2EngineSchedulerRemote(params)
	if err != nil {
		level.Error(logger).Log("msg", "v2 query execution failed", "error", err)
		return fmt.Errorf("v2 query execution failed: %w", err)
	}
	return checkResult(result)
}

// checkResult processes and displays query results
func checkResult(result logqlmodel.Result) error {
	streams, ok := result.Data.(logqlmodel.Streams)
	if !ok {
		return errors.New("unexpected response type")
	}
	level.Info(logger).Log("msg", "query results", "stream_count", len(streams))
	for _, stream := range streams {
		firstTs := stream.Entries[0].Timestamp
		level.Info(logger).Log("msg", "stream result", "timestamp", firstTs, "labels", stream.Labels)
	}
	return nil
}

// doLocalQueryWithV2Engine executes a query using the V2 engine
func doLocalQueryWithV2Engine(params logql.LiteralParams) (logqlmodel.Result, error) {
	logger := glog.NewLogfmtLogger(os.Stderr)
	ms := metastore.NewObjectMetastore(
		MustDataobjBucket(),
		metastore.Config{IndexStoragePrefix: "index/v0"},
		logger,
		metastore.NewObjectMetastoreMetrics(prometheus.DefaultRegisterer),
	)

	ctx := user.InjectOrgID(context.Background(), orgID)
	qe := engine.NewBasic(engine.ExecutorConfig{BatchSize: 512}, ms, MustDataobjBucket(), logql.NoLimits, prometheus.DefaultRegisterer, logger)
	query := qe.Query(params)
	return query.Exec(ctx)
}

// doLocalQueryWithV1Engine executes a query using the V1 engine
func doLocalQueryWithV1Engine(params logql.LiteralParams) (logqlmodel.Result, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)

	l := &validation.Limits{}
	flagext.DefaultValues(l)
	l.QueryReadyIndexNumDays = 7

	overrides, err := validation.NewOverrides(*l, nil)
	if err != nil {
		return logqlmodel.Result{}, err
	}

	err = os.MkdirAll("temp_index_cache", 0o755)
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("failed to create temp index cache directory: %w", err)
	}

	store, err := storage.NewStore(storage.Config{
		ObjectStore: bucket.ConfigWithNamedStores{
			Config: bucket.Config{
				GCS: gcs.Config{
					BucketName: storageBucket,
				},
			},
		},
		UseThanosObjstore: true,
		MaxChunkBatchSize: 24,
		TSDBShipperConfig: indexshipper.Config{
			Mode: indexshipper.ModeReadOnly,
			IndexGatewayClientConfig: indexgateway.ClientConfig{
				Mode:    "simple",
				Address: "",
			},
			CacheLocation:     "./temp_index_cache",
			CacheTTL:          time.Hour,
			ResyncInterval:    time.Hour,
			QueryReadyNumDays: 7,
		},
	}, config.ChunkStoreConfig{}, config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: model.TimeFromUnix(time.Now().Add(-time.Hour * 24 * 7).Unix())},
				IndexType:  "tsdb",
				ObjectType: "gcs",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: indexStoragePrefix,
						Period: time.Hour * 24,
					},
					PathPrefix: "index/",
				},
			},
		},
	}, overrides, storage.NewClientMetrics(), prometheus.DefaultRegisterer, glog.NewLogfmtLogger(os.Stderr), "loki")
	if err != nil {
		level.Error(logger).Log("msg", "failed to create storage", "error", err)
		return logqlmodel.Result{}, fmt.Errorf("failed to create storage: %w", err)
	}

	quer, err := querier.New(querier.Config{
		QueryStoreOnly: true,
	}, store, nil, overrides, deletion.NewNoOpDeleteRequestsClient(), glog.NewLogfmtLogger(os.Stderr))
	if err != nil {
		return logqlmodel.Result{}, err
	}

	qe := logql.NewEngine(logql.EngineOpts{}, quer, logql.NoLimits, glog.NewLogfmtLogger(os.Stderr))
	query := qe.Query(params)
	return query.Exec(ctx)
}

func doLocalQueryWithV2EngineScheduler(params logql.LiteralParams) (logqlmodel.Result, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)

	sched, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:        glog.With(logger, "component", "scheduler"),
		AdvertiseAddr: nil,
	})
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("creating scheduler: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, sched.Service()); err != nil {
		return logqlmodel.Result{}, fmt.Errorf("starting scheduler service: %w", err)
	}

	b := MustDataobjBucket()

	metastoreMetrics := metastore.NewObjectMetastoreMetrics(prometheus.DefaultRegisterer)
	msConfig := metastore.Config{IndexStoragePrefix: "index/v0"}
	workerLogger := glog.With(logger, "component", "worker")
	worker, err := engine.NewWorker(engine.WorkerParams{
		Logger:         workerLogger,
		AdvertiseAddr:  nil,
		Bucket:         b,
		Metastore:      metastore.NewObjectMetastore(b, msConfig, workerLogger, metastoreMetrics),
		LocalScheduler: sched,
		Config: engine.WorkerConfig{
			SchedulerLookupAddress:  "",
			SchedulerLookupInterval: 60,
			WorkerThreads:           64,
		},
		Executor: engine.ExecutorConfig{
			BatchSize: 128,
		},
	})
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("creating worker: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, worker.Service()); err != nil {
		return logqlmodel.Result{}, fmt.Errorf("starting worker service: %w", err)
	}

	engineLogger := glog.With(logger, "component", "engine")
	e, err := engine.New(engine.Params{
		Logger:     engineLogger,
		Registerer: prometheus.NewRegistry(),
		Config: engine.ExecutorConfig{
			BatchSize: 128,
		},
		Metastore: metastore.NewObjectMetastore(b, msConfig, engineLogger, metastoreMetrics),
		Scheduler: sched,
		Limits:    logql.NoLimits,
	})
	if err != nil {
		return logqlmodel.Result{}, err
	}

	return e.Execute(ctx, params)
}

func doLocalQueryWithV2EngineSchedulerRemote(params logql.LiteralParams) (logqlmodel.Result, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)

	schedSrv, schedSvc, err := newServerService("scheduler", 3101, logger, prometheus.NewRegistry())
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("failed to create scheduler server: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, schedSvc); err != nil {
		return logqlmodel.Result{}, fmt.Errorf("starting scheduler service: %w", err)
	}
	sched, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:        glog.With(logger, "component", "scheduler"),
		AdvertiseAddr: schedSrv.HTTPListenAddr(),
	})
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("creating scheduler: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, sched.Service()); err != nil {
		return logqlmodel.Result{}, fmt.Errorf("starting scheduler service: %w", err)
	}
	sched.RegisterSchedulerServer(schedSrv.HTTP)

	// worker
	workerSrv, workerSvc, err := newServerService("worker", 3102, logger, prometheus.NewRegistry())
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("failed to create worker server: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, workerSvc); err != nil {
		return logqlmodel.Result{}, fmt.Errorf("starting worker service: %w", err)
	}

	b := MustDataobjBucket()
	metastoreMetrics := metastore.NewObjectMetastoreMetrics(prometheus.DefaultRegisterer)
	msConfig := metastore.Config{IndexStoragePrefix: "index/v0"}
	workerLogger := glog.With(logger, "component", "worker")
	worker, err := engine.NewWorker(engine.WorkerParams{
		Logger:         workerLogger,
		AdvertiseAddr:  workerSrv.HTTPListenAddr(),
		Bucket:         b,
		Metastore:      metastore.NewObjectMetastore(b, msConfig, workerLogger, metastoreMetrics),
		LocalScheduler: nil,
		Config: engine.WorkerConfig{
			SchedulerLookupAddress:  schedSrv.HTTPListenAddr().String(),
			SchedulerLookupInterval: 60,
			WorkerThreads:           64,
		},
		Executor: engine.ExecutorConfig{
			BatchSize: 128,
		},
	})
	if err != nil {
		return logqlmodel.Result{}, fmt.Errorf("creating worker: %w", err)
	} else if err := services.StartAndAwaitRunning(ctx, worker.Service()); err != nil {
		return logqlmodel.Result{}, fmt.Errorf("starting worker service: %w", err)
	}
	worker.RegisterWorkerServer(workerSrv.HTTP)

	engineLogger := glog.With(logger, "component", "engine")
	e, err := engine.New(engine.Params{
		Logger:     engineLogger,
		Registerer: prometheus.NewRegistry(),
		Config: engine.ExecutorConfig{
			BatchSize: 128,
		},
		Metastore: metastore.NewObjectMetastore(b, msConfig, engineLogger, metastoreMetrics),
		Scheduler: sched,
		Limits:    logql.NoLimits,
	})
	if err != nil {
		return logqlmodel.Result{}, err
	}

	return e.Execute(ctx, params)
}

func newServerService(name string, httpPort int, logger glog.Logger, registerer prometheus.Registerer) (*server.Server, services.Service, error) {
	logger = glog.With(logger, "component", "server", "server", name)
	serv, err := server.New(server.Config{
		Log:               logger,
		Registerer:        registerer,
		HTTPListenNetwork: "tcp",
		HTTPListenAddress: "localhost",
		HTTPListenPort:    httpPort,
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
