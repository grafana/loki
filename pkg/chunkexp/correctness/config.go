package correctness

import (
	"flag"
	"fmt"
	"net/url"
	"time"
)

const (
	DefaultQueryInterval  = 15 * time.Second
	DefaultSettle         = 30 * time.Second
	DefaultMaxLookback    = 15 * time.Minute
	DefaultLagGrace       = 3 * time.Minute
	DefaultRetryInterval  = 5 * time.Second
	DefaultRequestTimeout = 15 * time.Second
	DefaultCycleTimeout   = 4 * time.Minute
	DefaultLogQueryLimit  = 100
)

// Config holds configuration for comparing stable Loki samples to the experimental stack.
type Config struct {
	StableEndpoint string        `yaml:"stable_endpoint"`
	ExpEndpoint    string        `yaml:"exp_endpoint"`
	TenantID       string        `yaml:"tenant_id"`
	QueryInterval  time.Duration `yaml:"query_interval"`
	Settle         time.Duration `yaml:"settle"`
	MaxLookback    time.Duration `yaml:"max_lookback"`
	LagGrace       time.Duration `yaml:"lag_grace"`
	RetryInterval  time.Duration `yaml:"retry_interval"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
	CycleTimeout   time.Duration `yaml:"cycle_timeout"`
	LogQueryLimit  int           `yaml:"log_query_limit"`
	StartedAt      time.Time     `yaml:"started_at"`
}

// RegisterFlags registers configuration flags.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}
	f.StringVar(&c.StableEndpoint, "chunk-exp-correctness.stable-endpoint", "", "Base URL of the stable Loki query API, for example http://query-frontend.loki-dev-005.svc.cluster.local.:3100.")
	f.StringVar(&c.ExpEndpoint, "chunk-exp-correctness.exp-endpoint", "", "Base URL of the experimental Loki query API.")
	f.StringVar(&c.TenantID, "chunk-exp-correctness.tenant-id", "", "Tenant ID sent as X-Scope-OrgID.")
	f.DurationVar(&c.QueryInterval, "chunk-exp-correctness.query-interval", DefaultQueryInterval, "Delay between correctness cycles.")
	f.DurationVar(&c.Settle, "chunk-exp-correctness.settle", DefaultSettle, "How far behind now a sample window ends, so the stable write path has accepted the line.")
	f.DurationVar(&c.MaxLookback, "chunk-exp-correctness.max-lookback", DefaultMaxLookback, "Maximum lookback from now. Combined with started-at as max(started-at, now-max-lookback).")
	f.DurationVar(&c.LagGrace, "chunk-exp-correctness.lag-grace", DefaultLagGrace, "How long to retry a missing line on the experimental querier before counting it incorrect.")
	f.DurationVar(&c.RetryInterval, "chunk-exp-correctness.retry-interval", DefaultRetryInterval, "Delay between experimental querier retries.")
	f.DurationVar(&c.RequestTimeout, "chunk-exp-correctness.request-timeout", DefaultRequestTimeout, "HTTP timeout for each query.")
	f.DurationVar(&c.CycleTimeout, "chunk-exp-correctness.cycle-timeout", DefaultCycleTimeout, "Timeout for a full correctness cycle, including retries.")
	f.IntVar(&c.LogQueryLimit, "chunk-exp-correctness.log-query-limit", DefaultLogQueryLimit, "Maximum log lines requested when selecting a sample.")
	f.Var((*rfc3339Time)(&c.StartedAt), "chunk-exp-correctness.started-at", "Do not sample lines before this time (RFC3339). Defaults to process start.")
}

// Validate checks constraints and applies defaults for zero-valued durations.
func (c *Config) Validate() error {
	if _, err := url.ParseRequestURI(c.StableEndpoint); err != nil {
		return fmt.Errorf("stable_endpoint: %w", err)
	}
	if _, err := url.ParseRequestURI(c.ExpEndpoint); err != nil {
		return fmt.Errorf("exp_endpoint: %w", err)
	}
	if c.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if c.QueryInterval <= 0 {
		c.QueryInterval = DefaultQueryInterval
	}
	if c.Settle < 0 {
		return fmt.Errorf("settle must be >= 0")
	}
	if c.MaxLookback <= 0 {
		c.MaxLookback = DefaultMaxLookback
	}
	if c.LagGrace < 0 {
		return fmt.Errorf("lag_grace must be >= 0")
	}
	if c.RetryInterval <= 0 {
		c.RetryInterval = DefaultRetryInterval
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = DefaultRequestTimeout
	}
	if c.CycleTimeout <= 0 {
		c.CycleTimeout = DefaultCycleTimeout
	}
	if c.LogQueryLimit <= 0 {
		c.LogQueryLimit = DefaultLogQueryLimit
	}
	return nil
}

type rfc3339Time time.Time

func (t *rfc3339Time) String() string {
	if t == nil || time.Time(*t).IsZero() {
		return ""
	}
	return time.Time(*t).UTC().Format(time.RFC3339)
}

func (t *rfc3339Time) Set(s string) error {
	if s == "" {
		*t = rfc3339Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	*t = rfc3339Time(parsed)
	return nil
}
