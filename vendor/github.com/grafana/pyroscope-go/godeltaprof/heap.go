package godeltaprof

import (
	"io"
	"runtime"
	"sync"

	"github.com/grafana/pyroscope-go/godeltaprof/internal/pprof"
)

// HeapProfiler is a stateful profiler for heap allocations in Go programs.
// It is based on runtime.MemProfile and provides similar functionality to
// pprof.WriteHeapProfile, but with some key differences.
//
// The HeapProfiler tracks the delta of heap allocations since the last
// profile was written, effectively providing a snapshot of the changes
// in heap usage between two points in time. This is in contrast to the
// pprof.WriteHeapProfile function, which accumulates profiling data
// and results in profiles that represent the entire lifetime of the program.
//
// The HeapProfiler is safe for concurrent use, as it serializes access to
// its internal state using a sync.Mutex. This ensures that multiple goroutines
// can call the Profile method without causing any data race issues.
//
// Usage:
//
//	hp := godeltaprof.NewHeapProfiler()
//	...
//	err := hp.Profile(someWriter)
type HeapProfiler struct {
	impl    pprof.DeltaHeapProfiler
	mutex   sync.Mutex
	options pprof.ProfileBuilderOptions
	gz      gz
}

func NewHeapProfiler() *HeapProfiler {
	return &HeapProfiler{
		impl: pprof.DeltaHeapProfiler{},
		options: pprof.ProfileBuilderOptions{
			GenericsFrames: true,
			LazyMapping:    true,
		}}
}

func NewHeapProfilerWithOptions(options ProfileOptions) *HeapProfiler {
	return &HeapProfiler{
		impl: pprof.DeltaHeapProfiler{},
		options: pprof.ProfileBuilderOptions{
			GenericsFrames: options.GenericsFrames,
			LazyMapping:    options.LazyMappings,
		},
	}
}

func (d *HeapProfiler) Profile(w io.Writer) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	p := pprof.MemProfile(true)
	rate := int64(runtime.MemProfileRate)

	zw := d.gz.get(w)
	b := pprof.NewProfileBuilder(w, zw, &d.options, pprof.HeapProfileConfig(rate))

	return d.impl.WriteHeapProto(b, p, rate)
}
