// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package testutil

import (
	"testing"
)

// TB represents union of test and benchmark.
// This allows the same test suite to be run by both benchmark and test, helping to reuse more code.
// The reason is that usually benchmarks are not being run on CI, especially for short tests, so you need to recreate
// usually similar tests for `Test<Name>(t *testing.T)` methods. Example of usage is presented here:
//
//	 func TestTestOrBench(t *testing.T) {
//		tb := NewTB(t)
//		tb.Run("1", func(tb TB) { testorbenchComplexTest(tb) })
//		tb.Run("2", func(tb TB) { testorbenchComplexTest(tb) })
//	}
//
//	func BenchmarkTestOrBench(b *testing.B) {
//		tb := NewTB(t)
//		tb.Run("1", func(tb TB) { testorbenchComplexTest(tb) })
//		tb.Run("2", func(tb TB) { testorbenchComplexTest(tb) })
//	}
type TB interface {
	testing.TB
	IsBenchmark() bool
	Run(name string, f func(t TB)) bool

	SetBytes(n int64)
	N() int

	ResetTimer()
	StartTimer()
	StopTimer()

	ReportAllocs()
	ReportMetric(n float64, unit string)
}

// tb implements TB as well as testing.TB interfaces.
type tb struct {
	testing.TB
}

// NewTB creates tb from testing.TB.
func NewTB(t testing.TB) TB { return &tb{TB: t} }

// Run benchmarks/tests f as a subbenchmark/subtest with the given name. It reports
// whether there were any failures.
//
// A subbenchmark/subtest is like any other benchmark/test.
func (t *tb) Run(name string, f func(t TB)) bool {
	if b, ok := t.TB.(*testing.B); ok {
		return b.Run(name, func(nested *testing.B) { f(&tb{TB: nested}) })
	}
	if t, ok := t.TB.(*testing.T); ok {
		return t.Run(name, func(nested *testing.T) { f(&tb{TB: nested}) })
	}
	panic("not a benchmark and not a test")
}

// N returns number of iterations to do for benchmark, 1 in case of test.
func (t *tb) N() int {
	if b, ok := t.TB.(*testing.B); ok {
		return b.N
	}
	return 1
}

// SetBytes records the number of bytes processed in a single operation for benchmark, noop otherwise.
// If this is called, the benchmark will report ns/op and MB/s.
func (t *tb) SetBytes(n int64) {
	if b, ok := t.TB.(*testing.B); ok {
		b.SetBytes(n)
	}
}

// ResetTimer resets a timer, if it's a benchmark, noop otherwise.
func (t *tb) ResetTimer() {
	if b, ok := t.TB.(*testing.B); ok {
		b.ResetTimer()
	}
}

// StartTimer starts a timer, if it's a benchmark, noop otherwise.
func (t *tb) StartTimer() {
	if b, ok := t.TB.(*testing.B); ok {
		b.StartTimer()
	}
}

// StopTimer stops a timer, if it's a benchmark, noop otherwise.
func (t *tb) StopTimer() {
	if b, ok := t.TB.(*testing.B); ok {
		b.StopTimer()
	}
}

// IsBenchmark returns true if it's a benchmark.
func (t *tb) IsBenchmark() bool {
	_, ok := t.TB.(*testing.B)
	return ok
}

// ReportAllocs reports allocs if it's a benchmark, noop otherwise.
func (t *tb) ReportAllocs() {
	if b, ok := t.TB.(*testing.B); ok {
		b.ReportAllocs()
	}
}

// ReportMetric reports metrics if it's a benchmark, noop otherwise.
func (t *tb) ReportMetric(n float64, unit string) {
	if b, ok := t.TB.(*testing.B); ok {
		b.ReportMetric(n, unit)
	}
}
