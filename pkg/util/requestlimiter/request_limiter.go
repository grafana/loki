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

// RequestLimiter reserves bytes of inflight budget for a request.
type RequestLimiter interface {
	Reserve(ctx context.Context, maxSize int64) (*Reservation, error)
}

// New returns an active limiter when MaxInflightBytes > 0, otherwise a no-op.
// onHeld is called with the signed byte delta whenever the held count changes
// (positive on reserve, negative on adjust/release); may be nil.
func New(cfg Config, onHeld func(int64)) RequestLimiter {
	if cfg.MaxInflightBytes == 0 {
		return noopRequestLimiter{}
	}
	return &inflightBytesLimiter{
		cfg:  cfg,
		pool: newPool(cfg.MaxInflightBytes, onHeld),
	}
}

type inflightBytesLimiter struct {
	cfg  Config
	pool *pool
}

func (l *inflightBytesLimiter) Reserve(ctx context.Context, maxSize int64) (*Reservation, error) {
	ctx, cancel := context.WithTimeout(ctx, l.cfg.MaxWait)
	defer cancel()
	return l.pool.reserve(ctx, maxSize)
}

type noopRequestLimiter struct{}

func (noopRequestLimiter) Reserve(_ context.Context, _ int64) (*Reservation, error) {
	return &Reservation{}, nil
}

// pool tracks the shared byte budget.
type pool struct {
	sem    *semaphore.Weighted
	onHeld func(int64) // called with +n on reserve, −delta on adjust/release; may be nil
}

func newPool(limit int64, onHeld func(int64)) *pool {
	return &pool{sem: semaphore.NewWeighted(limit), onHeld: onHeld}
}

func (p *pool) reserve(ctx context.Context, n int64) (*Reservation, error) {
	if err := p.sem.Acquire(ctx, n); err != nil {
		return nil, err
	}
	if p.onHeld != nil {
		p.onHeld(n)
	}
	return &Reservation{pool: p, held: n}, nil
}

// Reservation holds bytes pre-reserved from a pool.
// Its methods are safe to call concurrently.
type Reservation struct {
	mu   sync.Mutex
	pool *pool
	held int64
}

// AdjustToActual releases the over-estimate (held − actual) back to the pool
// once the true decompressed size is known. No-op if actual ≥ held.
func (r *Reservation) AdjustToActual(actual int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	overage := r.held - actual
	if overage <= 0 {
		return
	}
	if r.pool != nil {
		r.pool.sem.Release(overage)
		if r.pool.onHeld != nil {
			r.pool.onHeld(-overage)
		}
	}
	r.held = actual
}

// Release returns all remaining held bytes to the pool.
// Safe to call multiple times and without a prior AdjustToActual.
func (r *Reservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held == 0 {
		return
	}
	if r.pool != nil {
		r.pool.sem.Release(r.held)
		if r.pool.onHeld != nil {
			r.pool.onHeld(-r.held)
		}
	}
	r.held = 0
}
