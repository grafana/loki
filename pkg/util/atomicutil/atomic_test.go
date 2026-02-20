package atomicutil

import (
	"math"
	"testing"
	"time"
)

func TestFloat64(t *testing.T) {
	f := NewFloat64(0)
	if got := f.Load(); got != 0 {
		t.Errorf("expected 0, got %f", got)
	}

	f.Store(3.0)
	if got := f.Load(); got != 3.0 {
		t.Errorf("expected 3.0, got %f", got)
	}

	f.Add(1.0)
	if got := f.Load(); got != 4.0 {
		t.Errorf("expected 4.0, got %f", got)
	}

	if !f.CompareAndSwap(4.0, 5.0) {
		t.Error("CompareAndSwap should have succeeded")
	}
	if got := f.Load(); got != 5.0 {
		t.Errorf("expected 5.0, got %f", got)
	}

	if f.CompareAndSwap(4.0, 6.0) {
		t.Error("CompareAndSwap should have failed")
	}

	// Test with special values
	inf := NewFloat64(math.Inf(1))
	if got := inf.Load(); !math.IsInf(got, 1) {
		t.Errorf("expected +Inf, got %f", got)
	}

	negInf := NewFloat64(math.Inf(-1))
	if got := negInf.Load(); !math.IsInf(got, -1) {
		t.Errorf("expected -Inf, got %f", got)
	}
}

func TestDuration(t *testing.T) {
	d := NewDuration(0)
	if got := d.Load(); got != 0 {
		t.Errorf("expected 0, got %v", got)
	}

	d.Store(time.Second)
	if got := d.Load(); got != time.Second {
		t.Errorf("expected 1s, got %v", got)
	}

	d.Add(500 * time.Millisecond)
	if got := d.Load(); got != 1500*time.Millisecond {
		t.Errorf("expected 1.5s, got %v", got)
	}
}
