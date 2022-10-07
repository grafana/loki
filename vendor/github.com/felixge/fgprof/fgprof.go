// fgprof is a sampling Go profiler that allows you to analyze On-CPU as well
// as [Off-CPU](http://www.brendangregg.com/offcpuanalysis.html) (e.g. I/O)
// time together.
package fgprof

import (
	"io"
	"runtime"
	"strings"
	"time"
)

// Start begins profiling the goroutines of the program and returns a function
// that needs to be invoked by the caller to stop the profiling and write the
// results to w using the given format.
func Start(w io.Writer, format Format) func() error {
	// Go's CPU profiler uses 100hz, but 99hz might be less likely to result in
	// accidental synchronization with the program we're profiling.
	const hz = 99
	ticker := time.NewTicker(time.Second / hz)
	stopCh := make(chan struct{})

	stackCounts := stackCounter{}
	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stackCounts.Update()
			case <-stopCh:
				return
			}
		}
	}()

	return func() error {
		stopCh <- struct{}{}
		return writeFormat(w, stackCounts, format, hz)
	}
}

type stackCounter map[string]int

func (s stackCounter) Update() {
	// Determine the runtime.Frame of this func so we can hide it from our
	// profiling output.
	rpc := make([]uintptr, 1)
	n := runtime.Callers(1, rpc)
	if n < 1 {
		panic("could not determine selfFrame")
	}
	selfFrame, _ := runtime.CallersFrames(rpc).Next()

	// COPYRIGHT: The code for populating `p` below is copied from
	// writeRuntimeProfile in src/runtime/pprof/pprof.go.
	//
	// Find out how many records there are (GoroutineProfile(nil)),
	// allocate that many records, and get the data.
	// There's a race—more records might be added between
	// the two calls—so allocate a few extra records for safety
	// and also try again if we're very unlucky.
	// The loop should only execute one iteration in the common case.
	var p []runtime.StackRecord
	n, ok := runtime.GoroutineProfile(nil)
	for {
		// Allocate room for a slightly bigger profile,
		// in case a few more entries have been added
		// since the call to ThreadProfile.
		p = make([]runtime.StackRecord, n+10)
		n, ok = runtime.GoroutineProfile(p)
		if ok {
			p = p[0:n]
			break
		}
		// Profile grew; try again.
	}

outer:
	for _, pp := range p {
		frames := runtime.CallersFrames(pp.Stack())

		var stack []string
		for {
			frame, more := frames.Next()
			if !more {
				break
			} else if frame.Entry == selfFrame.Entry {
				continue outer
			}

			stack = append([]string{frame.Function}, stack...)
		}
		key := strings.Join(stack, ";")
		s[key]++
	}
}
