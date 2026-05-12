package requestlimiter

import (
	"context"
	"flag"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// Config holds the inflight-bytes load-shedding configuration.
type Config struct {
	MaxInflightBytes int64         `yaml:"max_inflight_bytes"`
	MaxWait          time.Duration `yaml:"max_wait"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.Int64Var(&cfg.MaxInflightBytes, prefix+".max-inflight-bytes", 0, "Maximum total bytes of decompressed requests in flight. Requests that would exceed this are load shed after waiting up to max-wait. 0 disables the limit.")
	fs.DurationVar(&cfg.MaxWait, prefix+".max-wait", 100*time.Millisecond, "How long to wait for inflight budget before load shedding a request.")
}

// Limiter gates concurrent inflight bytes. Construct with [New].
type Limiter struct {
	cfg    Config
	sem    *semaphore.Weighted // nil when MaxInflightBytes == 0 (no limit)
	onHeld func(int64)         // called with signed byte delta on each change; may be nil
}

// New returns a Limiter. When MaxInflightBytes is 0 the limiter is a no-op.
// onHeld is called with the signed byte delta whenever the held count changes
// (positive on reserve, negative on adjust/release); may be nil.
func New(cfg Config, onHeld func(int64)) *Limiter {
	l := &Limiter{cfg: cfg, onHeld: onHeld}
	if cfg.MaxInflightBytes > 0 {
		l.sem = semaphore.NewWeighted(cfg.MaxInflightBytes)
	}
	return l
}

// Reserve pre-acquires up to maxSize bytes, blocking up to MaxWait.
// The caller must eventually call Release on the returned Reservation.
func (l *Limiter) Reserve(ctx context.Context, maxSize int64) (*Reservation, error) {
	if l.sem == nil {
		return &Reservation{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, l.cfg.MaxWait)
	defer cancel()
	if err := l.sem.Acquire(ctx, maxSize); err != nil {
		return nil, err
	}
	if l.onHeld != nil {
		l.onHeld(maxSize)
	}
	return &Reservation{sem: l.sem, onHeld: l.onHeld, held: maxSize}, nil
}

// Reservation holds bytes pre-reserved from a Limiter.
// Its methods are safe to call concurrently.
type Reservation struct {
	mu     sync.Mutex
	sem    *semaphore.Weighted // nil for noop reservations
	onHeld func(int64)
	held   int64
}

// AdjustToActual releases the over-estimate (held − actual) back to the Limiter
// once the true decompressed size is known. No-op if actual ≥ held.
func (r *Reservation) AdjustToActual(actual int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	overage := r.held - actual
	if overage <= 0 {
		return
	}
	if r.sem != nil {
		r.sem.Release(overage)
		if r.onHeld != nil {
			r.onHeld(-overage)
		}
	}
	r.held = actual
}

// Release returns all remaining held bytes to the Limiter.
// Safe to call multiple times and without a prior AdjustToActual.
func (r *Reservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held == 0 {
		return
	}
	if r.sem != nil {
		r.sem.Release(r.held)
		if r.onHeld != nil {
			r.onHeld(-r.held)
		}
	}
	r.held = 0
}
