package executor

import (
	"runtime"
	"runtime/metrics"
	"testing"
	"time"
)

// heapPeak holds the largest heap sizes seen while a function runs, in bytes
// above the sizes before it started.
type heapPeak struct {
	// heap is the size of all heap objects, live or not yet freed.
	heap uint64
	// live is the heap size that the last GC cycle marked live.
	live uint64
}

// benchPeakHeap runs fn once per benchmark iteration and reports the largest
// heap sizes seen while fn runs, as peak-heap-B/op and peak-live-B/op.
func benchPeakHeap(b *testing.B, fn func()) {
	b.ReportAllocs()
	var peak heapPeak
	for b.Loop() {
		p := measurePeakHeap(fn)
		peak.heap = max(peak.heap, p.heap)
		peak.live = max(peak.live, p.live)
	}
	b.ReportMetric(float64(peak.heap), "peak-heap-B/op")
	b.ReportMetric(float64(peak.live), "peak-live-B/op")
}

// measurePeakHeap runs fn and returns the largest heap sizes seen while it
// runs.
//
// measurePeakHeap polls the runtime metrics every millisecond, because the Go
// heap profile only shows the heap at the last GC and misses the peak inside
// fn. The sizes exclude memory that lives across the call, such as the source
// objects in an in-memory bucket.
func measurePeakHeap(fn func()) heapPeak {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/heap/live:bytes"},
	}
	read := func() (heap, live uint64) {
		metrics.Read(samples)
		return samples[0].Value.Uint64(), samples[1].Value.Uint64()
	}

	runtime.GC()
	baseHeap, baseLive := read()

	done := make(chan struct{})
	result := make(chan heapPeak)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var peak heapPeak
		for {
			heap, live := read()
			peak.heap = max(peak.heap, heap)
			peak.live = max(peak.live, live)
			select {
			case <-done:
				result <- heapPeak{
					heap: peak.heap - min(peak.heap, baseHeap),
					live: peak.live - min(peak.live, baseLive),
				}
				return
			case <-ticker.C:
			}
		}
	}()

	fn()
	close(done)
	return <-result
}
