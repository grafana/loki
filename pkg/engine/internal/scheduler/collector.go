package scheduler

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/ewma"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// collector implements [prometheus.Collector], collecting metrics for a
// [Scheduler].
type collector struct {
	sched *Scheduler

	load              prometheus.GaugeFunc
	saturation        prometheus.GaugeFunc
	loadAverage       *ewma.EWMA
	saturationAverage *ewma.EWMA

	tasksInflight   *prometheus.Desc
	streamsInflight *prometheus.Desc

	connections *prometheus.Desc
	workers     *prometheus.Desc
	threads     *prometheus.Desc
}

var _ prometheus.Collector = (*collector)(nil)

// newCollector returns a new collector for the given scheduler. Load and
// saturation average metrics will only be computed after calling
// [collector.Process].
func newCollector(sched *Scheduler) *collector {
	var (
		loadSource       ewma.SourceFunc = func() float64 { return computeLoad(sched) }
		saturationSource ewma.SourceFunc = func() float64 { return computeSaturation(sched) }
	)

	return &collector{
		sched: sched,

		load: prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_load",
			Help: "Current load on the scheduler (count of running and pending tasks)",
		}, loadSource),
		saturation: prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_saturation",
			Help: "Current saturation of the scheduler (loki_engine_scheduler_load divided by non-idle worker threads)",
		}, saturationSource),
		loadAverage: ewma.MustNew(ewma.Options{
			Name: "loki_engine_scheduler_load_average",
			Help: "Average load on the scheduler over time",

			UpdateFrequency: 5 * time.Second,
			Windows:         []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute},
		}, loadSource),
		saturationAverage: ewma.MustNew(ewma.Options{
			Name: "loki_engine_scheduler_saturation_average",
			Help: "Average saturation of the scheduler over time",

			UpdateFrequency: 5 * time.Second,
			Windows:         []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute},
		}, saturationSource),

		tasksInflight: prometheus.NewDesc(
			"loki_engine_scheduler_tasks_inflight",
			"Number of in-flight tasks by state",
			[]string{"state"},
			nil,
		),
		streamsInflight: prometheus.NewDesc(
			"loki_engine_scheduler_streams_inflight",
			"Number of in-flight streams by state",
			[]string{"state"},
			nil,
		),

		connections: prometheus.NewDesc(
			"loki_engine_scheduler_connections_active",
			"Current number active connections to the scheduler",
			nil,
			nil,
		),
		workers: prometheus.NewDesc(
			"loki_engine_scheduler_workers",
			"Current number of workers connected to the scheduler by state",
			[]string{"state"},
			nil,
		),
		threads: prometheus.NewDesc(
			"loki_engine_scheduler_threads",
			"Current number of worker threads connected to the scheduler by state",
			[]string{"state"},
			nil,
		),
	}
}

// computeLoad returns the active load on the scheduler: the sum of running and
// pending tasks.
func computeLoad(sched *Scheduler) float64 {
	sched.resourcesMut.RLock()
	defer sched.resourcesMut.RUnlock()

	var load uint64

	for _, t := range sched.tasks {
		if t.status.State == workflow.TaskStateRunning || t.status.State == workflow.TaskStatePending {
			load++
		}
	}

	return float64(load)
}

// computeSaturation returns the saturation of the scheduler: the load divided by
// the number of non-idle threads.
func computeSaturation(sched *Scheduler) float64 {
	// The value here may be slightly off due to the asynchronous nature of the
	// scheduler: the load may change while we're counting compute capacity, and
	// threads may change states while we're looking at other connections.
	//
	// However, we should still be providing a good enough approximation of the
	// scheduler's saturation.
	load := computeLoad(sched)

	var compute uint64

	sched.connections.Range(func(key, _ any) bool {
		_, ready, busy := countThreadStates(key.(*workerConn))
		compute += uint64(ready + busy)
		return true
	})

	if compute == 0 {
		// If we have no compute capacity, fall back to the load as our ratio.
		return float64(load)
	}
	return float64(load) / float64(compute)
}

// Process performs stat computations for EWMA metrics of the collector. Process
// runs until the provided context is canceled.
func (mc *collector) Process(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return mc.loadAverage.Monitor(ctx) })
	g.Go(func() error { return mc.saturationAverage.Monitor(ctx) })

	return g.Wait()
}

func (mc *collector) Collect(ch chan<- prometheus.Metric) {
	mc.load.Collect(ch)
	mc.saturation.Collect(ch)
	mc.loadAverage.Collect(ch)
	mc.saturationAverage.Collect(ch)

	mc.collectResourceStats(ch)
	mc.collectConnStats(ch)
}

func (mc *collector) collectResourceStats(ch chan<- prometheus.Metric) {
	mc.sched.resourcesMut.RLock()
	defer mc.sched.resourcesMut.RUnlock()

	var (
		tasksByState   = make(map[workflow.TaskState]int)
		streamsByState = make(map[workflow.StreamState]int)
	)

	for _, t := range mc.sched.tasks {
		tasksByState[t.status.State]++
	}
	for _, s := range mc.sched.streams {
		streamsByState[s.state]++
	}

	for state, count := range tasksByState {
		ch <- prometheus.MustNewConstMetric(mc.tasksInflight, prometheus.GaugeValue, float64(count), state.String())
	}
	for state, count := range streamsByState {
		ch <- prometheus.MustNewConstMetric(mc.streamsInflight, prometheus.GaugeValue, float64(count), state.String())
	}
}

func (mc *collector) collectConnStats(ch chan<- prometheus.Metric) {
	var (
		totalConnections int

		workersByState = make(map[workerState]int)
		threadsByState = make(map[workerState]int)
	)

	mc.sched.connections.Range(func(key, _ any) bool {
		wc := key.(*workerConn)

		// We only want to count connections which have been flagged as control
		// plane connections (meaning they will be assigned work, and are a part
		// of the set of compute).
		if wc.Type() != connectionTypeControlPlane {
			return true
		}

		workersByState[getWorkerState(wc)]++

		idle, ready, busy := countThreadStates(wc)
		threadsByState[workerStateIdle] += idle
		threadsByState[workerStateReady] += ready
		threadsByState[workerStateBusy] += busy

		totalConnections++
		return true
	})

	ch <- prometheus.MustNewConstMetric(mc.connections, prometheus.GaugeValue, float64(totalConnections))

	for state, count := range workersByState {
		ch <- prometheus.MustNewConstMetric(mc.workers, prometheus.GaugeValue, float64(count), state.String())
	}
	for state, count := range threadsByState {
		ch <- prometheus.MustNewConstMetric(mc.threads, prometheus.GaugeValue, float64(count), state.String())
	}
}

func (mc *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- mc.tasksInflight
	ch <- mc.streamsInflight
	ch <- mc.connections
	ch <- mc.workers
	ch <- mc.threads
}

type workerState int

const (
	workerStateIdle  workerState = iota // workerStateIdle is used when the worker is sleeping.
	workerStateReady                    // workerStateReady is used when a worker is ready for a task.
	workerStateBusy                     // workerStateBusy is used when a worker is executing a task.
)

func (s workerState) String() string {
	switch s {
	case workerStateIdle:
		return "idle"
	case workerStateReady:
		return "ready"
	case workerStateBusy:
		return "busy"
	}

	return ""
}

func getWorkerState(wc *workerConn) workerState {
	_, ready, busy := countThreadStates(wc)

	// Worker state is inferred from thread states in descending precedence
	// (busy > ready > idle).
	switch {
	case busy > 0:
		return workerStateBusy
	case ready > 0:
		return workerStateReady
	default:
		return workerStateIdle
	}
}

func countThreadStates(wc *workerConn) (idle, ready, busy int) {
	wc.mut.RLock()
	defer wc.mut.RUnlock()

	busy = len(wc.tasks)
	ready = wc.readyThreads
	idle = wc.maxThreads - ready - busy

	return idle, ready, busy
}
