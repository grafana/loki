package redis

import (
	"context"
	"time"

	imultidb "github.com/redis/go-redis/v9/internal/multidb"
)

// MultiDBHealthCheck is the interface for health checking databases.
//
// Each method reports whether the database is healthy together with the error
// that made it unhealthy (if any). A healthy result is (true, nil). An
// unhealthy result is (false, err) where err explains the failure so callers
// (e.g. health-check metrics) can record why a check failed; err may be nil
// when the check completed and simply determined the database is not healthy.
//
// Each call is one probe. A policy decides how many probes to run (see
// MultiDBHealthCheckConfig).
//
// A check must be safe for concurrent use: the background loop, failover,
// SetActiveDatabase and AddDatabase can probe at the same time, and the
// checks of one pass run concurrently. A pass starts every check; one that
// fails does not skip the others, it only cancels their context. For
// example, the lag-aware check runs even when PING fails, and with 3 probes
// it makes 3 REST calls per pass.
//
// A check runs on MultiDB's background loop, during construction, and inside
// failover and fallback probes. It must not call the control operations (the
// MultiDBCtrl methods, or Close): see MultiDBCtrl. A check that calls Close,
// for example, blocks forever, because Close waits for the loop the check is
// running on. The same holds for a health-check policy.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
type MultiDBHealthCheck interface {
	// CheckHealth checks if a standalone client is healthy.
	CheckHealth(ctx context.Context, client *Client) (bool, error)

	// CheckClusterHealth checks if a cluster client is healthy.
	CheckClusterHealth(ctx context.Context, client *ClusterClient) (bool, error)
}

// MultiDBHealthCheckConfig is how a policy probes one health check. A check
// provides it with a Config() MultiDBHealthCheckConfig method (the multidb
// package checks do; multidb.HealthCheckConfig is the same type). A check
// without Config() gets the defaults: 3 probes, 500ms apart, 3s timeout.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
type MultiDBHealthCheckConfig struct {
	// Probes is the number of probes per check. Default: 3.
	Probes int
	// Delay is the wait between probes. Default: 500ms.
	Delay time.Duration
	// Timeout bounds all probes of the check. Default: 3s. It is capped by
	// MultiDBOptions.HealthCheckTimeout.
	Timeout time.Duration
}

// Default probe settings for health checks (HEALTHY_ALL: every probe of every
// check must pass).
const (
	defaultMultiDBHealthCheckProbes       = imultidb.DefaultProbes
	defaultMultiDBHealthCheckProbeDelay   = imultidb.DefaultProbeDelay
	defaultMultiDBHealthCheckProbeTimeout = imultidb.DefaultProbeTimeout
)

// configurableMultiDBHealthCheck is a check with its own probe settings.
type configurableMultiDBHealthCheck interface {
	Config() MultiDBHealthCheckConfig
}

// multidbProbeConfig returns hc's probe settings: its Config() when it has
// one, else the defaults. Invalid values fall back to the defaults: Probes <= 0
// would pass trivially and Timeout <= 0 would expire at once.
func multidbProbeConfig(hc MultiDBHealthCheck) imultidb.ProbeConfig {
	cfg := MultiDBHealthCheckConfig{
		Probes:  defaultMultiDBHealthCheckProbes,
		Delay:   defaultMultiDBHealthCheckProbeDelay,
		Timeout: defaultMultiDBHealthCheckProbeTimeout,
	}
	if c, ok := hc.(configurableMultiDBHealthCheck); ok {
		cfg = c.Config()
	}
	if cfg.Probes <= 0 {
		cfg.Probes = defaultMultiDBHealthCheckProbes
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultMultiDBHealthCheckProbeTimeout
	}
	if cfg.Delay < 0 {
		cfg.Delay = defaultMultiDBHealthCheckProbeDelay
	}
	return imultidb.ProbeConfig(cfg)
}

// defaultMultiDBPolicy is the built-in policy, HEALTHY_ALL: the checks run
// concurrently and every probe of every check must pass. It is the same logic
// as multidb.HealthyAllPolicy, except that gating probes run one probe per
// check (see withGatingProbe).
type defaultMultiDBPolicy struct{}

func (defaultMultiDBPolicy) Execute(ctx context.Context, checks []MultiDBHealthCheck, client *Client) bool {
	return runHealthyAll(ctx, checks, func(ctx context.Context, hc MultiDBHealthCheck) (bool, error) {
		return hc.CheckHealth(ctx, client)
	})
}

func (defaultMultiDBPolicy) ExecuteCluster(ctx context.Context, checks []MultiDBHealthCheck, client *ClusterClient) bool {
	return runHealthyAll(ctx, checks, func(ctx context.Context, hc MultiDBHealthCheck) (bool, error) {
		return hc.CheckClusterHealth(ctx, client)
	})
}

// multidbGatingProbeKey marks the context of a gating probe: the synchronous
// probe of SetActiveDatabase and of ProbeTargetBeforeFailover. The default
// policy runs one probe per check there (Timeout unchanged). The full pass,
// 3 probes 500ms apart, holds failoverMu for over 1s and outlasts short
// command deadlines. Custom policies do not read it.
type multidbGatingProbeKey struct{}

// withGatingProbe marks ctx for a single-probe pass under the default policy.
func withGatingProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, multidbGatingProbeKey{}, true)
}

func isGatingProbe(ctx context.Context) bool {
	gating, _ := ctx.Value(multidbGatingProbeKey{}).(bool)
	return gating
}

func runHealthyAll(ctx context.Context, checks []MultiDBHealthCheck, probe func(context.Context, MultiDBHealthCheck) (bool, error)) bool {
	gating := isGatingProbe(ctx)
	return imultidb.RunChecks(ctx, len(checks), func(ctx context.Context, i int) bool {
		hc := checks[i]
		cfg := multidbProbeConfig(hc)
		if gating {
			cfg.Probes = 1
		}
		return imultidb.ProbesAllMustPass(ctx, cfg, func(ctx context.Context) (bool, error) {
			return probe(ctx, hc)
		})
	})
}
