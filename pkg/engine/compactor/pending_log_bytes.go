package compactor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// pendingLogBytes keeps the pending log bytes gauge of one tenant in step with
// one LogMerge phase.
//
// It labels every log input with its age at the plan time of the phase. A
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
		for input := range plan.logInputs() {
			bytesByAge[p.age(input)] += input.bytes
			total += input.bytes
		}
	}
	for age, bytes := range bytesByAge {
		gauge.WithLabelValues(tenant, age).Set(float64(bytes))
	}
	return p, total
}

// complete removes the log input bytes of plan from the gauge.
func (p pendingLogBytes) complete(plan logCompactionPlan) {
	for input := range plan.logInputs() {
		p.gauge.WithLabelValues(p.tenant, p.age(input)).Sub(float64(input.bytes))
	}
}

func (p pendingLogBytes) age(input logInput) string {
	return p.buckets.label(input.oldestTimestamp, p.planTime)
}
