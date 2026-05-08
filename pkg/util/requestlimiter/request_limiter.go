package requestlimiter

import (
	"context"
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/util/metric"
	"golang.org/x/sync/semaphore"
)

type Config struct {
	MaxInflightBytes int64         `yaml:"max_inflight_bytes"`
	MaxWait          time.Duration `yaml:"max_wait"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.Int64Var(&cfg.MaxInflightBytes, prefix+".max-inflight-bytes", 0, "Maximum inflight bytes. Requests that would take inflight bytes above this value will be load shed. Defaults to never load-shedding.")
	fs.DurationVar(&cfg.MaxWait, prefix+".max-wait", 100*time.Millisecond, "Maximum time waiting for the request size limiter before the request will be load shed.")
}

type RequestLimiter interface {
	Limit(ctx context.Context, requestSize int64) (func(), error)
}

type InflightBytesRequestLimiter struct {
	cfg                Config
	semaphore          semaphore.Weighted
	maxSampleCollector *metric.MaxSampleCollector
}

func New(cfg Config, maxSampleCollector *metric.MaxSampleCollector) RequestLimiter {
	if cfg.MaxInflightBytes == 0 {
		return NoopRequestLimiter{maxSampleCollector: maxSampleCollector}
	}
	return &InflightBytesRequestLimiter{
		cfg:                cfg,
		semaphore:          *semaphore.NewWeighted(cfg.MaxInflightBytes),
		maxSampleCollector: maxSampleCollector,
	}
}

func (r *InflightBytesRequestLimiter) Limit(ctx context.Context, requestSize int64) (func(), error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, r.cfg.MaxWait)
	defer cancel()
	err := r.semaphore.Acquire(ctxWithTimeout, requestSize)
	if err != nil {
		return nil, err // TODO timed out
	}
	r.maxSampleCollector.Inc(requestSize)
	return func() {
		r.maxSampleCollector.Inc(-requestSize)
		r.semaphore.Release(requestSize)
	}, nil
}

type NoopRequestLimiter struct {
	maxSampleCollector *metric.MaxSampleCollector
}

func (n NoopRequestLimiter) Limit(_ context.Context, requestSize int64) (func(), error) {
	n.maxSampleCollector.Inc(requestSize)
	return func() {
		n.maxSampleCollector.Inc(-requestSize)
	}, nil
}
