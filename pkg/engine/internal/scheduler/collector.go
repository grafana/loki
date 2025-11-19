package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// collector implements [prometheus.Collector], collecting metrics for a
// [Scheduler].
type collector struct {
	sched *Scheduler

	tasksInflight   *prometheus.Desc
	streamsInflight *prometheus.Desc

	connections *prometheus.Desc
	workers     *prometheus.Desc
	threads     *prometheus.Desc
}

var _ prometheus.Collector = (*collector)(nil)

func newCollector(sched *Scheduler) *collector {
	return &collector{
		sched: sched,

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

func (mc *collector) Collect(ch chan<- prometheus.Metric) {
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
