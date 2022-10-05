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

	prof := &profiler{}
	stackCounts := stackCounter{}

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stacks := prof.GoroutineProfile()
				stackCounts.Update(stacks)
			case <-stopCh:
				return
			}
		}
	}()

	return func() error {
		stopCh <- struct{}{}
		return writeFormat(w, stackCounts.HumanMap(prof.SelfFrame()), format, hz)
	}
}

// profiler provides a convenient and performant way to access
// runtime.GoroutineProfile().
type profiler struct {
	stacks    []runtime.StackRecord
	selfFrame *runtime.Frame
}

// GoroutineProfile returns the stacks of all goroutines currently managed by
// the scheduler. This includes both goroutines that are currently running
// (On-CPU), as well as waiting (Off-CPU).
func (p *profiler) GoroutineProfile() []runtime.StackRecord {
	if p.selfFrame == nil {
		// Determine the runtime.Frame of this func so we can hide it from our
		// profiling output.
		rpc := make([]uintptr, 1)
		n := runtime.Callers(1, rpc)
		if n < 1 {
			panic("could not determine selfFrame")
		}
		selfFrame, _ := runtime.CallersFrames(rpc).Next()
		p.selfFrame = &selfFrame
	}

	// We don't know how many goroutines exist, so we have to grow p.stacks
	// dynamically. We overshoot by 10% since it's possible that more goroutines
	// are launched in between two calls to GoroutineProfile. Once p.stacks
	// reaches the maximum number of goroutines used by the program, it will get
	// reused indefinitely, eliminating GoroutineProfile calls and allocations.
	//
	// TODO(fg) There might be workloads where it would be nice to shrink
	// p.stacks dynamically as well, but let's not over-engineer this until we
	// understand those cases better.
	for {
		n, ok := runtime.GoroutineProfile(p.stacks)
		if !ok {
			p.stacks = make([]runtime.StackRecord, int(float64(n)*1.1))
		} else {
			return p.stacks[0:n]
		}
	}
}

func (p *profiler) SelfFrame() *runtime.Frame {
	return p.selfFrame
}

type stringStackCounter map[string]int

func (s stringStackCounter) Update(p []runtime.StackRecord) {
	for _, pp := range p {
		frames := runtime.CallersFrames(pp.Stack())

		var stack []string
		for {
			frame, more := frames.Next()
			stack = append([]string{frame.Function}, stack...)
			if !more {
				break
			}
		}
		key := strings.Join(stack, ";")
		s[key]++
	}
}

type stackCounter map[[32]uintptr]int

func (s stackCounter) Update(p []runtime.StackRecord) {
	for _, pp := range p {
		s[pp.Stack0]++
	}
}

// @TODO(fg) create a better interface that avoids the pprof output having to
// split the stacks using the `;` separator.
func (s stackCounter) HumanMap(exclude *runtime.Frame) map[string]int {
	m := map[string]int{}
outer:
	for stack0, count := range s {
		frames := runtime.CallersFrames((&runtime.StackRecord{Stack0: stack0}).Stack())

		var stack []string
		for {
			frame, more := frames.Next()
			if frame.Entry == exclude.Entry {
				continue outer
			}
			stack = append([]string{frame.Function}, stack...)
			if !more {
				break
			}
		}
		key := strings.Join(stack, ";")
		m[key] = count
	}
	return m
}
