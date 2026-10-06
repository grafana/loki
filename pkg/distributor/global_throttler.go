package distributor

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	throttler "github.com/spiridonov/deadhorse"
	throttlerclient "github.com/spiridonov/deadhorse/client"

	"github.com/grafana/loki/v3/pkg/util/discovery"
)

// GlobalThrottlerConfig configures the external, distributed rate throttler
// used by the validation.ExactIngestionRateStrategy ingestion rate strategy.
// Unlike the ring-based "global" strategy, this mechanism needs no
// distributor-side ring: shard membership is tracked by polling DNS, and
// enforcement is exact rather than approximated by dividing a local limit by
// the healthy distributor count.
type GlobalThrottlerConfig struct {
	// Addresses is a comma-separated list of shard addresses (host:port),
	// and/or a dns+/dnssrv+/dnssrvnoa+ name -- resolved the same way as the
	// bloomgateway and indexgateway client configs (see
	// pkg/util/discovery.NewDNS).
	Addresses string `yaml:"addresses"`

	// DiscoveryInterval is how often the resolved address set is checked
	// for changes. Only an actual change to the (sorted) set triggers
	// rebuilding the client -- see globalThrottler.applyAddresses.
	DiscoveryInterval time.Duration `yaml:"discovery_interval"`

	// Timeout is the per-call network timeout to a shard.
	Timeout time.Duration `yaml:"timeout"`

	// FailOpen, if true (the default), treats an unreachable shard as not
	// throttled rather than failing the push -- a rate limiter should not
	// be why the write path goes down. Set to false only for limits that
	// are also a security/quota control, not just protective.
	FailOpen bool `yaml:"fail_open"`

	// BreakerFailureThreshold and BreakerOpenDuration configure the
	// per-shard circuit breaker: after this many consecutive failures a
	// shard is failed fast (no network activity) for BreakerOpenDuration
	// before a single trial call probes it again. BreakerFailureThreshold
	// <= 0 disables the breaker.
	BreakerFailureThreshold int           `yaml:"breaker_failure_threshold"`
	BreakerOpenDuration     time.Duration `yaml:"breaker_open_duration"`
}

func (cfg *GlobalThrottlerConfig) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.StringVar(&cfg.Addresses, prefix+".addresses", "", "Comma-separated list of global throttler shard addresses (host:port), or a dns+/dnssrv+/dnssrvnoa+ name to resolve. Required when -distributor.ingestion-rate-limit-strategy=exact.")
	fs.DurationVar(&cfg.DiscoveryInterval, prefix+".discovery-interval", 30*time.Second, "How often to re-resolve "+prefix+".addresses and, on an actual change, rebuild the client.")
	fs.DurationVar(&cfg.Timeout, prefix+".timeout", 10*time.Millisecond, "Per-call network timeout to a global throttler shard.")
	fs.BoolVar(&cfg.FailOpen, prefix+".fail-open", true, "Treat an unreachable global throttler shard as not throttled rather than failing the push.")
	fs.IntVar(&cfg.BreakerFailureThreshold, prefix+".breaker-failure-threshold", 5, "Consecutive failures to a shard before its circuit breaker opens. 0 or less disables the breaker.")
	fs.DurationVar(&cfg.BreakerOpenDuration, prefix+".breaker-open-duration", time.Second, "How long a shard's circuit breaker stays open (failing fast) before a trial call probes it again.")
}

// globalThrottler holds a client for the external rate-throttler fleet,
// keeping it current with the fleet's actual membership via periodic DNS
// resolution -- there is no ring, gossip, or membership protocol on either
// side, so this is the only thing standing in for "handle membership
// changes" that a ring would otherwise provide.
//
// The current client is held in an atomic.Pointer and only ever replaced as
// a whole, never mutated -- the same pattern pkg/logline/store's Store uses
// for its Snapshot. A concurrent Throttle call never blocks on a refresh in
// progress, and a refresh never blocks a concurrent Throttle call.
type globalThrottler struct {
	services.Service

	cfg    GlobalThrottlerConfig
	dns    discovery.DNS
	logger log.Logger

	client    atomic.Pointer[throttlerClientRef]
	lastAddrs []string // only ever read/written from the single refresh goroutine
}

// newGlobalThrottler resolves cfg.Addresses once, synchronously, and fails
// fast if nothing resolves -- turning what would otherwise be a panic deep
// inside client.NewShardedClient on the first push into a clean startup
// error instead. The returned service still needs to be added to the
// distributor's services.Manager by the caller.
func newGlobalThrottler(cfg GlobalThrottlerConfig, logger log.Logger, registerer prometheus.Registerer) (*globalThrottler, error) {
	t := &globalThrottler{
		cfg:    cfg,
		dns:    discovery.NewDNS(logger, cfg.DiscoveryInterval, cfg.Addresses, registerer),
		logger: logger,
	}

	if err := t.applyAddresses(t.dns.Addresses()); err != nil {
		t.dns.Stop()
		return nil, err
	}

	t.Service = services.NewTimerService(cfg.DiscoveryInterval, nil, t.refresh, t.stop).WithName("global throttler discovery")
	return t, nil
}

// refresh is the recurring timer tick. Unlike the initial, synchronous
// resolution in newGlobalThrottler, this never fails the service: a
// transient empty or unreachable resolution is logged and otherwise
// ignored, keeping whatever client is currently in place rather than
// tearing down a working one over a DNS blip. Returning an error here would
// fail the whole distributor (see services.NewTimerService), which a
// passing DNS hiccup must never do.
func (t *globalThrottler) refresh(_ context.Context) error {
	if err := t.applyAddresses(t.dns.Addresses()); err != nil {
		level.Warn(t.logger).Log("msg", "global throttler: failed to apply resolved addresses, keeping the current client", "err", err)
	}
	return nil
}

// applyAddresses sorts addrs, and -- only if the sorted set actually
// differs from the last one applied -- builds a fresh ShardedClient and
// atomically swaps it in, closing the superseded one after. Sorting first
// means a DNS answer returning the same set in a different order (not
// guaranteed stable across lookups) never triggers a spurious rebuild: an
// unconditional rebuild on every tick would silently reset every shard's
// circuit breaker to closed each time, retrying a known-dead shard right
// when the breaker exists to stop that.
func (t *globalThrottler) applyAddresses(addrs []string) error {
	if len(addrs) == 0 {
		return fmt.Errorf("global throttler: no addresses resolved for %q", t.cfg.Addresses)
	}

	sorted := append([]string(nil), addrs...)
	sort.Strings(sorted)

	if slices.Equal(sorted, t.lastAddrs) {
		return nil
	}

	opts := []throttlerclient.Option{
		throttlerclient.WithTimeout(t.cfg.Timeout),
		throttlerclient.WithBreaker(t.cfg.BreakerFailureThreshold, t.cfg.BreakerOpenDuration),
	}
	if !t.cfg.FailOpen {
		opts = append(opts, throttlerclient.WithFailClosed())
	}

	old := t.client.Swap(&throttlerClientRef{client: throttlerclient.NewShardedClient(sorted, opts...)})
	t.lastAddrs = sorted
	if old != nil {
		old.retire()
	}
	return nil
}

// Throttle delegates to the currently active client. Callers never see a
// nil client: newGlobalThrottler only returns successfully after the first
// applyAddresses call has stored one.
func (t *globalThrottler) Throttle(ctx context.Context, tenantID string, entries []throttler.RequestEntry) ([]throttler.ResponseEntry, error) {
	for {
		ref := t.client.Load()
		resp, err := ref.throttle(ctx, tenantID, entries)
		if !errors.Is(err, errThrottlerClientRetired) {
			return resp, err
		}
		// ref was retired between Load and the call. If a swap retired it,
		// its replacement is already stored, so loading again makes progress;
		// if it is still current we are shutting down and must not spin.
		if t.client.Load() == ref {
			return nil, err
		}
	}
}

var errThrottlerClientRetired = errors.New("global throttler: client retired")

func (t *globalThrottler) stop(_ error) error {
	t.dns.Stop()
	t.client.Load().retire()
	return nil
}

// throttlerClientRef wraps a ShardedClient so it can be closed without
// failing calls that already picked it up. Closing a client tears down its
// connections, and a call failing that way is admitted without charging any
// bucket under fail_open -- so closing mid-call would briefly disable
// enforcement on every DNS membership change.
type throttlerClientRef struct {
	mu     sync.RWMutex // held for read for the duration of each call
	closed bool
	client *throttlerclient.ShardedClient
}

// throttle runs the call, returning errThrottlerClientRetired without
// calling if the client has already been retired. In-flight calls are bounded by the client's
// per-call timeout, so retire never waits long.
func (r *throttlerClientRef) throttle(ctx context.Context, tenantID string, entries []throttler.RequestEntry) ([]throttler.ResponseEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errThrottlerClientRetired
	}
	return r.client.Throttle(ctx, tenantID, entries)
}

// retire waits for in-flight calls to finish, then closes the client. It
// must only be called after the ref has been swapped out of globalThrottler.client,
// except at shutdown.
func (r *throttlerClientRef) retire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.client.Close()
}
