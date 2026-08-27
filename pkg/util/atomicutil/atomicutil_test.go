package atomicutil

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestIntConstructors(t *testing.T) {
	if got := NewInt64(0).Load(); got != 0 {
		t.Fatalf("NewInt64(0)=%d", got)
	}
	if got := NewInt64(-1).Load(); got != -1 {
		t.Fatalf("NewInt64(-1)=%d", got)
	}
	if got := NewInt32(7).Load(); got != 7 {
		t.Fatalf("NewInt32(7)=%d", got)
	}
	if got := NewUint32(9).Load(); got != 9 {
		t.Fatalf("NewUint32(9)=%d", got)
	}
	if got := NewUint64(11).Load(); got != 11 {
		t.Fatalf("NewUint64(11)=%d", got)
	}
	if NewBool(false).Load() {
		t.Fatal("NewBool(false)")
	}
	if !NewBool(true).Load() {
		t.Fatal("NewBool(true)")
	}
}

func TestDuration(t *testing.T) {
	d := NewDuration(0)
	if d.Load() != 0 {
		t.Fatalf("zero load: %v", d.Load())
	}
	d.Store(time.Second)
	if d.Load() != time.Second {
		t.Fatalf("store: %v", d.Load())
	}
	if got := d.Add(time.Second); got != 2*time.Second {
		t.Fatalf("add: %v", got)
	}
	if got := d.Sub(500 * time.Millisecond); got != 1500*time.Millisecond {
		t.Fatalf("sub: %v", got)
	}
	if !d.CompareAndSwap(1500*time.Millisecond, time.Minute) {
		t.Fatal("cas")
	}
	if d.Swap(0) != time.Minute {
		t.Fatal("swap")
	}
}

func TestFloat64(t *testing.T) {
	f := NewFloat64(math.Inf(1))
	if !math.IsInf(f.Load(), 1) {
		t.Fatalf("load inf: %v", f.Load())
	}
	f.Store(1.5)
	if f.Load() != 1.5 {
		t.Fatalf("store: %v", f.Load())
	}
	if !f.CompareAndSwap(1.5, 2.5) {
		t.Fatal("cas")
	}
	if got := f.Add(0.5); got != 3 {
		t.Fatalf("add: %v", got)
	}
	nan := math.NaN()
	f.Store(nan)
	if !f.CompareAndSwap(nan, 1) {
		t.Fatal("cas nan")
	}
}

func TestString(t *testing.T) {
	var s String
	if s.Load() != "" {
		t.Fatalf("zero: %q", s.Load())
	}
	s.Store("fqn")
	if s.Load() != "fqn" {
		t.Fatalf("store: %q", s.Load())
	}
	if !s.CompareAndSwap("fqn", "next") {
		t.Fatal("cas")
	}
	if s.Swap("last") != "next" {
		t.Fatal("swap")
	}
}

func TestError(t *testing.T) {
	var e Error
	if e.Load() != nil {
		t.Fatalf("zero: %v", e.Load())
	}
	err := errors.New("boom")
	e.Store(err)
	if e.Load() != err {
		t.Fatalf("store: %v", e.Load())
	}
	e.Store(nil)
	if e.Load() != nil {
		t.Fatalf("store nil: %v", e.Load())
	}
}
