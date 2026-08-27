package distributor

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/grafana/dskit/httpgrpc"
)

type WriteFaultInjection struct {
	InternalErrorEnabled bool          `yaml:"internal_error_enabled"`
	InternalErrorPeriod  int           `yaml:"internal_error_period"`
	LatencyEnabled       bool          `yaml:"latency_enabled"`
	Latency              time.Duration `yaml:"latency"`
}

func (cfg *WriteFaultInjection) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.InternalErrorEnabled, "distributor.write-internal-error-injection.enabled", false, "Return HTTP 500 / gRPC Internal for every Nth distributor write. For rollout testing only.")
	f.IntVar(&cfg.InternalErrorPeriod, "distributor.write-internal-error-injection.period", 2, "Return an internal error for every N distributor writes.")
	f.BoolVar(&cfg.LatencyEnabled, "distributor.write-latency-injection.enabled", false, "Add a fixed delay to every distributor write. For rollout testing only.")
	f.DurationVar(&cfg.Latency, "distributor.write-latency-injection.duration", 2*time.Second, "Delay added to every distributor write when write latency injection is enabled.")
}

func (cfg WriteFaultInjection) Validate() error {
	if cfg.InternalErrorEnabled && cfg.InternalErrorPeriod <= 0 {
		return fmt.Errorf("-distributor.write-internal-error-injection.period must be greater than 0 when write internal error injection is enabled")
	}
	if cfg.LatencyEnabled && cfg.Latency <= 0 {
		return fmt.Errorf("-distributor.write-latency-injection.duration must be greater than 0 when write latency injection is enabled")
	}
	return nil
}

func (cfg WriteFaultInjection) Enabled() bool {
	return cfg.InternalErrorEnabled || cfg.LatencyEnabled
}

type writeFaultInjector struct {
	cfg      WriteFaultInjection
	requests atomic.Uint64
}

func newWriteFaultInjector(cfg WriteFaultInjection) *writeFaultInjector {
	return &writeFaultInjector{cfg: cfg}
}

func (i *writeFaultInjector) maybe(ctx context.Context) error {
	if i == nil || !i.cfg.Enabled() {
		return nil
	}
	if i.cfg.LatencyEnabled && i.cfg.Latency > 0 {
		timer := time.NewTimer(i.cfg.Latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if !i.cfg.InternalErrorEnabled {
		return nil
	}
	n := i.requests.Add(1)
	if n%uint64(i.cfg.InternalErrorPeriod) != 0 {
		return nil
	}
	return httpgrpc.Errorf(http.StatusInternalServerError, "distributor write internal error injection")
}
