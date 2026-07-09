// Command dummy-scheduler exercises the v2 engine scheduler/worker infrastructure
// using synthetic DummyLoad tasks. It starts multiple schedulers and workers
// inside a single process, submits a configurable number of workflows, and
// reports throughput.
//
// Two wire transports are supported (-wire flag):
//
//   - http2 (default): schedulers and workers communicate over real HTTP/2
//     connections on loopback. Supports multiple schedulers and workers.
//
//   - local: pure in-process channels via [wire.LocalNetwork]. No OS sockets
//     are used. Because there is only one scheduler address in a LocalNetwork,
//     -schedulers is clamped to 1 in this mode.
//
// All workflows are log-shaped (TopK descending by timestamp):
//
//	TopK  ←  Parallelize  ←  DummyLoad (Parallelism shards)
//
// The workflow planner splits DummyLoad into Parallelism independent shard
// tasks, each of which runs on a worker thread, mirroring how DataObjScan
// parallelization works with real data.
//
// Usage:
//
//	go run ./tools/engine/dummy-scheduler \
//	  -wire=local -workers=10 -threads=32 \
//	  -workflows=200 -parallelism=8 \
//	  -batches=20 -batch-size=1024 -sleep=0
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"text/tabwriter"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/prometheus/client_golang/prometheus"
)

const tenantID = "dummy"

var (
	flagWire        = flag.String("wire", "local", `wire transport between scheduler and workers: "http2" or "local"`)
	flagSchedulers  = flag.Int("schedulers", 2, "number of schedulers to start (between 1–7); clamped to 1 for -wire=local")
	flagWorkers     = flag.Int("workers", 100, "number of workers to start")
	flagThreads     = flag.Int("threads", 32, "executor threads per worker")
	flagWorkflows   = flag.Int("workflows", 500, "total number of workflows to submit")
	flagParallelism = flag.Int("parallelism", 1000, "DummyLoad shard count per workflow (task fan-out)")
	flagBatches     = flag.Int("batches", 2, "DummyLoad batches per shard")
	flagBatchSize   = flag.Int("batch-size", 1, "rows per DummyLoad batch")
	flagSleep       = flag.Duration("sleep", 0, "artificial sleep per batch (simulates slow I/O)")
	flagVerbose     = flag.Bool("v", false, "enable debug-level logging")
)

func main() {
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger := gokitlog.NewLogfmtLogger(gokitlog.NewSyncWriter(os.Stderr))
	if !*flagVerbose {
		logger = level.NewFilter(logger, level.AllowInfo())
	}

	ctx = user.InjectOrgID(ctx, tenantID)

	if err := run(ctx, logger); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

type schedNode struct {
	sched   *engine.Scheduler
	addrStr string           // "host:port" (http2) or "local" (local wire)
	srv     *server.Server   // nil in local mode
	svc     services.Service // nil in local mode
}

type workerNode struct {
	w   *engine.Worker
	srv *server.Server   // nil in local mode
	svc services.Service // nil in local mode
}

func run(ctx context.Context, logger gokitlog.Logger) error {
	useLocal := *flagWire == "local"

	numSched := clamp(*flagSchedulers, 1, 7)
	if useLocal && numSched > 1 {
		level.Warn(logger).Log("msg", "local wire supports only one scheduler address; clamping -schedulers to 1")
		numSched = 1
	}

	// For local mode, a single LocalNetwork is shared by all schedulers and
	// workers so they can dial each other via in-process channels.
	var localNet *engine.LocalNetwork
	if useLocal {
		localNet = engine.NewLocalNetwork()
	}

	// Start schedulers
	scheds := make([]*schedNode, numSched)
	for i := range numSched {
		node, err := startScheduler(ctx, gokitlog.With(logger, "sched", i), localNet)
		if err != nil {
			return fmt.Errorf("scheduler %d: %w", i, err)
		}
		scheds[i] = node
		level.Info(logger).Log("msg", "scheduler ready", "id", i, "addr", node.addrStr)
	}

	// Start workers. Distribute workers round-robin across schedulers.
	workers := make([]*workerNode, *flagWorkers)
	for i := range *flagWorkers {
		targetSched := scheds[i%numSched]
		node, err := startWorker(ctx, gokitlog.With(logger, "worker", i), targetSched, localNet)
		if err != nil {
			return fmt.Errorf("worker %d: %w", i, err)
		}
		workers[i] = node
		level.Info(logger).Log("msg", "worker ready", "id", i, "scheduler", targetSched.addrStr)
	}

	// Brief pause so workers complete their handshake before we flood them.
	time.Sleep(300 * time.Millisecond)

	// Submit workflows
	stats := &globalStats{}
	totalStart := time.Now()

	level.Info(logger).Log(
		"msg", "submitting workflows",
		"total", *flagWorkflows,
	)

	var wg sync.WaitGroup
	for i := range *flagWorkflows {
		// Round-robin across schedulers so all see traffic.
		sched := scheds[i%numSched].sched

		wg.Go(func() {
			if err := runWorkflow(ctx, logger, sched, stats); err != nil && ctx.Err() == nil {
				level.Warn(logger).Log("msg", "workflow error", "err", err)
			}
		})
	}

	wg.Wait()
	elapsed := time.Since(totalStart)

	// Graceful shutdown
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()

	for i, w := range workers {
		if err := services.StopAndAwaitTerminated(shutCtx, w.w.Service()); err != nil {
			level.Warn(logger).Log("msg", "worker shutdown error", "id", i, "err", err)
		}
		if w.svc != nil {
			if err := services.StopAndAwaitTerminated(shutCtx, w.svc); err != nil {
				level.Warn(logger).Log("msg", "worker server shutdown error", "id", i, "err", err)
			}
		}
	}
	for i, s := range scheds {
		if err := services.StopAndAwaitTerminated(shutCtx, s.sched.Service()); err != nil {
			level.Warn(logger).Log("msg", "scheduler shutdown error", "id", i, "err", err)
		}
		if s.svc != nil {
			if err := services.StopAndAwaitTerminated(shutCtx, s.svc); err != nil {
				level.Warn(logger).Log("msg", "scheduler server shutdown error", "id", i, "err", err)
			}
		}
	}

	// Report
	printResults(elapsed, stats)

	return nil
}

// Component constructors

// startScheduler starts a scheduler. When localNet is non-nil the scheduler
// uses in-process local wire transport and no HTTP server is started.
func startScheduler(ctx context.Context, logger gokitlog.Logger, localNet *engine.LocalNetwork) (*schedNode, error) {
	if localNet != nil {
		return startSchedulerLocal(ctx, logger, localNet)
	}
	return startSchedulerHTTP2(ctx, logger)
}

func startSchedulerHTTP2(ctx context.Context, logger gokitlog.Logger) (*schedNode, error) {
	srv, svc, err := newServerService("scheduler", logger, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, svc); err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}

	sched, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:        gokitlog.With(logger, "component", "scheduler"),
		AdvertiseAddr: srv.HTTPListenAddr(),
	})
	if err != nil {
		return nil, fmt.Errorf("new scheduler: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, sched.Service()); err != nil {
		return nil, fmt.Errorf("start scheduler: %w", err)
	}

	sched.RegisterSchedulerServer(srv.HTTP)

	return &schedNode{
		sched:   sched,
		addrStr: srv.HTTPListenAddr().String(),
		srv:     srv,
		svc:     svc,
	}, nil
}

func startSchedulerLocal(ctx context.Context, logger gokitlog.Logger, localNet *engine.LocalNetwork) (*schedNode, error) {
	sched, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:       gokitlog.With(logger, "component", "scheduler"),
		LocalNetwork: localNet,
	})
	if err != nil {
		return nil, fmt.Errorf("new scheduler: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, sched.Service()); err != nil {
		return nil, fmt.Errorf("start scheduler: %w", err)
	}
	return &schedNode{sched: sched, addrStr: "local"}, nil
}

// startWorker starts a worker. When localNet is non-nil the worker uses
// in-process local wire transport and no HTTP server is started.
func startWorker(ctx context.Context, logger gokitlog.Logger, targetSched *schedNode, localNet *engine.LocalNetwork) (*workerNode, error) {
	if localNet != nil {
		return startWorkerLocal(ctx, logger, targetSched, localNet)
	}
	return startWorkerHTTP2(ctx, logger, targetSched)
}

func startWorkerHTTP2(ctx context.Context, logger gokitlog.Logger, targetSched *schedNode) (*workerNode, error) {
	srv, svc, err := newServerService("worker", logger, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, svc); err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}

	w, err := engine.NewWorker(engine.WorkerParams{
		Logger:        gokitlog.With(logger, "component", "worker"),
		AdvertiseAddr: srv.HTTPListenAddr(),
		Config: engine.WorkerConfig{
			WorkerThreads:           *flagThreads,
			SchedulerLookupAddress:  targetSched.addrStr,
			SchedulerLookupInterval: time.Minute,
		},
		Executor: engine.ExecutorConfig{
			BatchSize: 512,
		},
	}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("new worker: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, w.Service()); err != nil {
		return nil, fmt.Errorf("start worker: %w", err)
	}

	w.RegisterWorkerServer(srv.HTTP)

	return &workerNode{w: w, srv: srv, svc: svc}, nil
}

func startWorkerLocal(ctx context.Context, logger gokitlog.Logger, targetSched *schedNode, localNet *engine.LocalNetwork) (*workerNode, error) {
	w, err := engine.NewWorker(engine.WorkerParams{
		Logger:         gokitlog.With(logger, "component", "worker"),
		LocalScheduler: targetSched.sched,
		LocalNetwork:   localNet,
		Config: engine.WorkerConfig{
			WorkerThreads: *flagThreads,
		},
		Executor: engine.ExecutorConfig{
			BatchSize: 512,
		},
	}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("new worker: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, w.Service()); err != nil {
		return nil, fmt.Errorf("start worker: %w", err)
	}
	return &workerNode{w: w}, nil
}

// newServerService creates a dskit HTTP server on a random loopback port with
// HTTP/2 enabled (via h2c for cleartext). The returned service manages the
// server lifecycle.
func newServerService(name string, logger gokitlog.Logger, reg prometheus.Registerer) (*server.Server, services.Service, error) {
	logger = gokitlog.With(logger, "component", "server", "server", name)
	srv, err := server.New(server.Config{
		Log:               logger,
		Registerer:        reg,
		HTTPListenNetwork: "tcp",
		HTTPListenAddress: "127.0.0.1",
		HTTPListenPort:    0,
	})
	if err != nil {
		return nil, nil, err
	}

	// Enable HTTP/2 over cleartext so the wire transport can use h2 streams.
	//srv.HTTPServer.Handler = h2c.NewHandler(srv.HTTPServer.Handler, &http2.Server{})

	done := make(chan error, 1)
	svc := services.NewBasicService(
		nil,
		func(ctx context.Context) error {
			level.Info(logger).Log("msg", "server starting", "addr", srv.HTTPListenAddr())
			go func() {
				defer close(done)
				done <- srv.Run()
			}()
			select {
			case <-ctx.Done():
				return nil
			case err := <-done:
				return err
			}
		},
		func(_ error) error {
			level.Info(logger).Log("msg", "server shutting down")
			srv.Shutdown()
			<-done
			return nil
		},
	)

	return srv, svc, nil
}

// Workflow execution

type globalStats struct {
	completed  atomic.Int64
	failed     atomic.Int64
	batchCount atomic.Int64
	rowCount   atomic.Int64
}

func runWorkflow(
	ctx context.Context,
	logger gokitlog.Logger,
	sched *engine.Scheduler,
	stats *globalStats,
) error {
	rows, batches, err := engine.RunDummyWorkflow(ctx, sched, engine.DummyWorkflowParams{
		Tenant:        tenantID,
		NumBatches:    *flagBatches,
		BatchSize:     *flagBatchSize,
		SleepPerBatch: *flagSleep,
		Parallelism:   *flagParallelism,
		TopK:          10_000,
		Logger:        logger,
	})
	if err != nil {
		stats.failed.Add(1)
		fmt.Println(err)
		return err
	}
	stats.completed.Add(1)
	stats.rowCount.Add(rows)
	stats.batchCount.Add(batches)
	return nil
}

// Helper functions

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

func printResults(elapsed time.Duration, stats *globalStats) {
	completed := stats.completed.Load()
	failed := stats.failed.Load()
	batches := stats.batchCount.Load()
	rows := stats.rowCount.Load()

	var rowsPerSec float64
	if elapsed > 0 {
		rowsPerSec = float64(rows) / elapsed.Seconds()
	}

	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "=== dummy-scheduler results ===")
	_, _ = fmt.Fprintf(w, "elapsed\t%v\n", elapsed.Round(time.Millisecond))
	_, _ = fmt.Fprintf(w, "workflows\t%d ok / %d failed / %d total\n", completed, failed, completed+failed)
	_, _ = fmt.Fprintf(w, "batches read\t%d\n", batches)
	_, _ = fmt.Fprintf(w, "rows read\t%d\n", rows)
	_, _ = fmt.Fprintf(w, "throughput\t%.0f rows/s\n", rowsPerSec)
	_ = w.Flush()
}
