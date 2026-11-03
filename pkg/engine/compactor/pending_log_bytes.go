package compactor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// pendingLogBytes keeps the pending log bytes gauge of one tenant in step with
// one LogMerge phase.
//
// It labels the log stats of every task with its age at the plan time of the phase. A
// completed plan subtracts its bytes at the same time, so they leave the same
// buckets that they joined, however long the phase runs.
type pendingLogBytes struct {
	gauge    *prometheus.GaugeVec // tenant, age
	tenant   string
	buckets  logAgeBuckets
	planTime time.Time
}

// startPendingLogBytes sets every age bucket of tenant to the log input bytes
// of plans at planTime. Buckets with no bytes are set to zero, so a bucket
// that empties between phases does not keep its old value.
//
// It returns the tracker for the phase and the total log input bytes.
func startPendingLogBytes(
	gauge *prometheus.GaugeVec,
	tenant string,
	buckets logAgeBuckets,
	planTime time.Time,
	plans []logCompactionPlan,
) (pendingLogBytes, uint64) {
	p := pendingLogBytes{gauge: gauge, tenant: tenant, buckets: buckets, planTime: planTime}

	bytesByAge := make(map[string]uint64)
	for _, age := range buckets.labels() {
		bytesByAge[age] = 0
	}
	var total uint64
	for _, plan := range plans {
		for stats := range plan.logTaskStats() {
			bytesByAge[p.age(stats)] += stats.inputBytes
			total += stats.inputBytes
		}
	}
	for age, bytes := range bytesByAge {
		gauge.WithLabelValues(tenant, age).Set(float64(bytes))
	}
	return p, total
}

// complete removes the log input bytes of plan from the gauge.
func (p pendingLogBytes) complete(plan logCompactionPlan) {
	for stats := range plan.logTaskStats() {
		p.gauge.WithLabelValues(p.tenant, p.age(stats)).Sub(float64(stats.inputBytes))
	}
}

func (p pendingLogBytes) age(stats logTaskStats) string {
	return p.buckets.label(stats.oldestTimestamp, p.planTime)
}
