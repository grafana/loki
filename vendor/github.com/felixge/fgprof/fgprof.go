// fgprof is a sampling Go profiler that allows you to analyze On-CPU as well
// as [Off-CPU](http://www.brendangregg.com/offcpuanalysis.html) (e.g. I/O)
// time together.
package fgprof

import (
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/pprof/profile"
)

// Format decides how the output is rendered to the user.
type Format string

const (
	// FormatFolded is used by Brendan Gregg's FlameGraph utility, see
	// https://github.com/brendangregg/FlameGraph#2-fold-stacks.
	FormatFolded Format = "folded"
	// FormatPprof is used by Google's pprof utility, see
	// https://github.com/google/pprof/blob/master/proto/README.md.
	FormatPprof Format = "pprof"
)

// Start begins profiling the goroutines of the program and returns a function
// that needs to be invoked by the caller to stop the profiling and write the
// results to w using the given format.
func Start(w io.Writer, format Format) func() error {
	startTime := time.Now()

	// Go's CPU profiler uses 100hz, but 99hz might be less likely to result in
	// accidental synchronization with the program we're profiling.
	const hz = 99
	ticker := time.NewTicker(time.Second / hz)
	stopCh := make(chan struct{})
	prof := &profiler{}
	profile := newWallclockProfile()

	var sampleCount int64

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				sampleCount++

				stacks := prof.GoroutineProfile()
				profile.Add(stacks)
			case <-stopCh:
				return
			}
		}
	}()

	return func() error {
		stopCh <- struct{}{}
		endTime := time.Now()
		profile.Ignore(prof.SelfFrames()...)

		// Compute actual sample rate in case, due to performance issues, we
		// were not actually able to sample at the given hz. Converting
		// everything to float avoids integers being rounded in the wrong
		// direction and improves the correctness of times in profiles.
		duration := endTime.Sub(startTime)
		actualHz := float64(sampleCount) / (float64(duration) / 1e9)
		return profile.Export(w, format, int(math.Round(actualHz)), startTime, endTime)
	}
}

// profiler provides a convenient and performant way to access
// runtime.GoroutineProfile().
type profiler struct {
	stacks    []runtime.StackRecord
	selfFrame *runtime.Frame
}

// nullTerminationWorkaround deals with a regression in go1.23, see:
// - https://github.com/felixge/fgprof/issues/33
// - https://go-review.googlesource.com/c/go/+/609815
var nullTerminationWorkaround = runtime.Version() == "go1.23.0"

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
		if nullTerminationWorkaround {
			for i := range p.stacks {
				p.stacks[i].Stack0 = [32]uintptr{}
			}
		}
		n, ok := runtime.GoroutineProfile(p.stacks)
		if !ok {
			p.stacks = make([]runtime.StackRecord, int(float64(n)*1.1))
		} else {
			return p.stacks[0:n]
		}
	}
}

// SelfFrames returns frames that belong to the profiler so that we can ignore
// them when exporting the final profile.
func (p *profiler) SelfFrames() []*runtime.Frame {
	if p.selfFrame != nil {
		return []*runtime.Frame{p.selfFrame}
	}
	return nil
}

func newWallclockProfile() *wallclockProfile {
	return &wallclockProfile{stacks: map[[32]uintptr]*wallclockStack{}}
}

// wallclockProfile holds a wallclock profile that can be exported in different
// formats.
type wallclockProfile struct {
	stacks map[[32]uintptr]*wallclockStack
	ignore []*runtime.Frame
}

// wallclockStack holds the symbolized frames of a stack trace and the number
// of times it has been seen.
type wallclockStack struct {
	frames []*runtime.Frame
	count  int
}

// Ignore sets a list of frames that should be ignored when exporting the
// profile.
func (p *wallclockProfile) Ignore(frames ...*runtime.Frame) {
	p.ignore = frames
}

// Add adds the given stack traces to the profile.
func (p *wallclockProfile) Add(stackRecords []runtime.StackRecord) {
	for _, stackRecord := range stackRecords {
		if _, ok := p.stacks[stackRecord.Stack0]; !ok {
			ws := &wallclockStack{}
			// symbolize pcs into frames
			frames := runtime.CallersFrames(stackRecord.Stack())
			for {
				frame, more := frames.Next()
				ws.frames = append(ws.frames, &frame)
				if !more {
					break
				}
			}
			p.stacks[stackRecord.Stack0] = ws
		}
		p.stacks[stackRecord.Stack0].count++
	}
}

func (p *wallclockProfile) Export(w io.Writer, f Format, hz int, startTime, endTime time.Time) error {
	switch f {
	case FormatFolded:
		return p.exportFolded(w)
	case FormatPprof:
		return p.exportPprof(hz, startTime, endTime).Write(w)
	default:
		return fmt.Errorf("unknown format: %q", f)
	}
}

// exportStacks returns the stacks in this profile except those that have been
// set to Ignore().
func (p *wallclockProfile) exportStacks() []*wallclockStack {
	stacks := make([]*wallclockStack, 0, len(p.stacks))
nextStack:
	for _, ws := range p.stacks {
		for _, f := range ws.frames {
			for _, igf := range p.ignore {
				if f.Entry == igf.Entry {
					continue nextStack
				}
			}
		}
		stacks = append(stacks, ws)
	}
	return stacks
}

func (p *wallclockProfile) exportFolded(w io.Writer) error {
	var lines []string
	stacks := p.exportStacks()
	for _, ws := range stacks {
		var foldedStack []string
		for _, f := range ws.frames {
			foldedStack = append(foldedStack, f.Function)
		}
		line := fmt.Sprintf("%s %d", strings.Join(foldedStack, ";"), ws.count)
		lines = append(lines, line)
	}
	sort.Strings(lines)
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

func (p *wallclockProfile) exportPprof(hz int, startTime, endTime time.Time) *profile.Profile {
	prof := &profile.Profile{}
	m := &profile.Mapping{ID: 1, HasFunctions: true}
	prof.Period = int64(1e9 / hz) // Number of nanoseconds between samples.
	prof.TimeNanos = startTime.UnixNano()
	prof.DurationNanos = int64(endTime.Sub(startTime))
	prof.Mapping = []*profile.Mapping{m}
	prof.SampleType = []*profile.ValueType{
		{
			Type: "samples",
			Unit: "count",
		},
		{
			Type: "time",
			Unit: "nanoseconds",
		},
	}
	prof.PeriodType = &profile.ValueType{
		Type: "wallclock",
		Unit: "nanoseconds",
	}

	type functionKey struct {
		Name     string
		Filename string
	}
	funcIdx := map[functionKey]*profile.Function{}

	type locationKey struct {
		Function functionKey
		Line     int
	}
	locationIdx := map[locationKey]*profile.Location{}
	for _, ws := range p.exportStacks() {
		sample := &profile.Sample{
			Value: []int64{
				int64(ws.count),
				int64(1000 * 1000 * 1000 / hz * ws.count),
			},
		}

		for _, frame := range ws.frames {
			fnKey := functionKey{Name: frame.Function, Filename: frame.File}
			function, ok := funcIdx[fnKey]
			if !ok {
				function = &profile.Function{
					ID:         uint64(len(prof.Function)) + 1,
					Name:       frame.Function,
					SystemName: frame.Function,
					Filename:   frame.File,
				}
				funcIdx[fnKey] = function
				prof.Function = append(prof.Function, function)
			}

			locKey := locationKey{Function: fnKey, Line: frame.Line}
			location, ok := locationIdx[locKey]
			if !ok {
				location = &profile.Location{
					ID:      uint64(len(prof.Location)) + 1,
					Mapping: m,
					Line: []profile.Line{{
						Function: function,
						Line:     int64(frame.Line),
					}},
				}
				locationIdx[locKey] = location
				prof.Location = append(prof.Location, location)
			}
			sample.Location = append(sample.Location, location)
		}
		prof.Sample = append(prof.Sample, sample)
	}
	return prof
}

type symbolizedStacks map[[32]uintptr][]frameCount

func (w wallclockProfile) Symbolize(exclude *runtime.Frame) symbolizedStacks {
	m := make(symbolizedStacks)
outer:
	for stack0, ws := range w.stacks {
		frames := runtime.CallersFrames((&runtime.StackRecord{Stack0: stack0}).Stack())

		for {
			frame, more := frames.Next()
			if frame.Entry == exclude.Entry {
				continue outer
			}
			m[stack0] = append(m[stack0], frameCount{Frame: &frame, Count: ws.count})
			if !more {
				break
			}
		}
	}
	return m
}

type frameCount struct {
	*runtime.Frame
	Count int
}
