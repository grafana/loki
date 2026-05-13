package inflightbytes

import (
	"context"
	"testing"
	"time"
)

func newTestLimiter(maxBytes int64) *Limiter {
	return New(Config{MaxInflightBytes: maxBytes, MaxWait: 10 * time.Millisecond}, nil)
}

func TestReservation_AdjustAndRelease(t *testing.T) {
	l := newTestLimiter(1000)
	res, err := l.Reserve(context.Background(), 800)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	// 200 free. Adjust to actual 300 → releases 500, leaving 700 free.
	res.AdjustToActual(300)
	res2, err2 := l.Reserve(context.Background(), 700)
	if err2 != nil {
		t.Fatalf("Reserve after AdjustToActual: %v", err2)
	}
	res2.Release()
	res.Release()
	// Full budget restored.
	res3, err3 := l.Reserve(context.Background(), 1000)
	if err3 != nil {
		t.Fatalf("Reserve after Release: %v", err3)
	}
	res3.Release()
}

func TestReservation_ReleaseIsIdempotent(t *testing.T) {
	l := newTestLimiter(100)
	res, _ := l.Reserve(context.Background(), 100)
	res.Release()
	res.Release() // must not panic or double-release
	if _, err := l.Reserve(context.Background(), 100); err != nil {
		t.Fatalf("budget not fully restored after double Release: %v", err)
	}
}

func TestReservation_AdjustActualLargerThanReserved(t *testing.T) {
	// actual > reserved is a no-op; Release must still free the original held amount.
	l := newTestLimiter(100)
	res, _ := l.Reserve(context.Background(), 50)
	res.AdjustToActual(200) // no-op
	res.Release()
	if _, err := l.Reserve(context.Background(), 100); err != nil {
		t.Fatalf("budget not restored: %v", err)
	}
}

func TestReservation_ExceedsBudgetBlocking(t *testing.T) {
	l := newTestLimiter(100)
	res, _ := l.Reserve(context.Background(), 100)
	defer res.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	if _, err := l.Reserve(ctx, 1); err == nil {
		t.Fatal("expected error when budget is full, got nil")
	}
}

func TestNoopReservation(t *testing.T) {
	res := &Reservation{}
	res.AdjustToActual(500) // must not panic
	res.Release()            // must not panic
	res.Release()            // idempotent
}
