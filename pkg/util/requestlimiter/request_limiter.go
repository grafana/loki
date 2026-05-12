package requestlimiter

import (
	"context"
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/util/limitedreader"
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
	Reserve(ctx context.Context, maxSize int64) (*limitedreader.Reservation, error)
	// InflightBytes returns the number of bytes currently held across all active reservations.
	InflightBytes() int64
}

// New returns an active limiter when MaxInflightBytes > 0, otherwise a no-op.
func New(cfg Config) RequestLimiter {
	if cfg.MaxInflightBytes == 0 {
		return noopRequestLimiter{}
	}
	return &inflightBytesLimiter{
		cfg:  cfg,
		pool: limitedreader.NewPool(cfg.MaxInflightBytes),
	}
}

type inflightBytesLimiter struct {
	cfg  Config
	pool *limitedreader.Pool
}

func (l *inflightBytesLimiter) Reserve(ctx context.Context, maxSize int64) (*limitedreader.Reservation, error) {
	ctx, cancel := context.WithTimeout(ctx, l.cfg.MaxWait)
	defer cancel()
	return l.pool.Reserve(ctx, maxSize)
}

func (l *inflightBytesLimiter) InflightBytes() int64 {
	return l.pool.InflightBytes()
}

type noopRequestLimiter struct{}

func (noopRequestLimiter) Reserve(_ context.Context, _ int64) (*limitedreader.Reservation, error) {
	return limitedreader.NewNoopReservation(), nil
}

func (noopRequestLimiter) InflightBytes() int64 { return 0 }
