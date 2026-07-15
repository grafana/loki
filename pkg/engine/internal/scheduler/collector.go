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

	load        prometheus.GaugeFunc
	loadAverage *ewma.EWMA

	tasksInflight   *prometheus.Desc
	streamsInflight *prometheus.Desc

	connections  *prometheus.Desc
	readyWorkers *prometheus.Desc
	taskQueue    *prometheus.Desc
}

var _ prometheus.Collector = (*collector)(nil)

// newCollector returns a new collector for the given scheduler. Load and
// saturation average metrics will only be computed after calling
// [collector.Process].
func newCollector(sched *Scheduler) *collector {
	var loadSource ewma.SourceFunc = func() float64 { return computeLoad(sched) }

	return &collector{
		sched: sched,

		load: prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_load",
			Help: "Current number of queued or assigned tasks without a terminal result",
		}, loadSource),
		loadAverage: ewma.MustNew(ewma.Options{
			Name: "loki_engine_scheduler_load_average",
			Help: "Average load on the scheduler over time",

			UpdateFrequency: 5 * time.Second,
			Windows:         []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute},
		}, loadSource),

		tasksInflight: prometheus.NewDesc(
			"loki_engine_scheduler_tasks_inflight",
			"Number of queued or assigned tasks without a terminal result",
			nil,
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

		readyWorkers: prometheus.NewDesc(
			"loki_engine_scheduler_ready_workers",
			"Current number of workers ready to accept tasks",
			nil,
			nil,
		),
		taskQueue: prometheus.NewDesc(
			"loki_engine_scheduler_task_queue_length",
			"Current number of tasks waiting in the assignment queue",
			nil,
			nil,
		),
	}
}

// computeLoad returns the number of queued or assigned tasks without a terminal
// result.
func computeLoad(sched *Scheduler) float64 {
	return float64(sched.metrics.activeLoad.Load())
}

// Process performs stat computations for EWMA metrics of the collector. Process
// runs until the provided context is canceled.
func (mc *collector) Process(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return mc.loadAverage.Monitor(ctx) })

	return g.Wait()
}

func (mc *collector) Collect(ch chan<- prometheus.Metric) {
	mc.load.Collect(ch)
	mc.loadAverage.Collect(ch)

	mc.collectResourceStats(ch)
	mc.collectConnStats(ch)
	mc.collectAssignStats(ch)
}

func (mc *collector) collectResourceStats(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(mc.tasksInflight, prometheus.GaugeValue, float64(mc.sched.metrics.activeLoad.Load()))

	guard := mc.sched.resourcesMut.RLock("collector_resource_stats")
	defer guard.RUnlock()

	streamsByState := make(map[workflow.StreamState]int)
	for _, s := range mc.sched.streams {
		streamsByState[s.state]++
	}

	for state, count := range streamsByState {
		ch <- prometheus.MustNewConstMetric(mc.streamsInflight, prometheus.GaugeValue, float64(count), state.String())
	}
}

func (mc *collector) collectConnStats(ch chan<- prometheus.Metric) {
	var totalConnections int

	mc.sched.connections.Range(func(key, _ any) bool {
		wc := key.(*workerConn)

		// We only want to count connections which have been flagged as control
		// plane connections (meaning they will be assigned work, and are a part
		// of the set of compute).
		if wc.Type() != connectionTypeControlPlane {
			return true
		}

		totalConnections++
		return true
	})

	ch <- prometheus.MustNewConstMetric(mc.connections, prometheus.GaugeValue, float64(totalConnections))
}

func (mc *collector) collectAssignStats(ch chan<- prometheus.Metric) {
	guard := mc.sched.assignMut.RLock("collector_assign_stats")
	defer guard.RUnlock()

	ch <- prometheus.MustNewConstMetric(mc.readyWorkers, prometheus.GaugeValue, float64(len(mc.sched.connectedWorkers)))
	ch <- prometheus.MustNewConstMetric(mc.taskQueue, prometheus.GaugeValue, float64(mc.sched.taskQueue.Len()))
}

func (mc *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- mc.tasksInflight
	ch <- mc.streamsInflight
	ch <- mc.connections
	ch <- mc.readyWorkers
	ch <- mc.taskQueue
}
