package scheduler

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/grafana/loki/v4/pkg/logline/store"
)

// benchMetas generates realistic metas matching the simulation's production
// profile: N builders flushing every ~4.2 minutes across a time window.
func benchMetas(n int, seed int64) []store.Meta {
	rng := rand.New(rand.NewSource(seed))
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	flushWindow := 4*time.Minute + 12*time.Second // ~4.2 min per builder flush

	metas := make([]store.Meta, n)
	for i := range metas {
		// Stagger across a 15-minute tick window.
		offset := time.Duration(float64(i) / float64(n) * float64(15*time.Minute))
		maxT := epoch.Add(offset)
		minT := maxT.Add(-flushWindow)
		metas[i] = store.Meta{
			Version:   "v3",
			Date:      "day-000",
			Hash:      fmt.Sprintf("%016x", i),
			SizeBytes: 45*mib + rng.Int63n(120*mib+1), // 45–165 MiB
			MinLogTs:  minT,
			MaxLogTs:  maxT,
		}
	}
	return metas
}

func BenchmarkOverlapPlanner(b *testing.B) {
	for _, n := range []int{54, 200, 500, 1000} {
		metas := benchMetas(n, 42)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			jobs := 0
			for b.Loop() {
				p := newOverlapPlanner(metas, 512*mib)
				jobs = 0
				for p.Plan() != nil {
					jobs++
				}
			}
			b.ReportMetric(float64(jobs), "jobs")
		})
	}
}

func BenchmarkDateGroupPlanner(b *testing.B) {
	for _, n := range []int{54, 200, 500, 1000} {
		metas := benchMetas(n, 42)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			jobs := 0
			for b.Loop() {
				p := newDateGroupPlanner(metas, 512*mib)
				jobs = 0
				for p.Plan() != nil {
					jobs++
				}
			}
			b.ReportMetric(float64(jobs), "jobs")
		})
	}
}
