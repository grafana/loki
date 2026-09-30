package correctness

import (
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultQueryInterval   = 5 * time.Second
	DefaultQueryRangeMin   = 5 * time.Minute
	DefaultQueryRangeMax   = 30 * time.Minute
	DefaultMaxLookback     = 30 * 24 * time.Hour
	DefaultRequestTimeout  = 15 * time.Second
	DefaultCycleTimeout    = 2 * time.Minute
	DefaultErrorBackoffMin = 5 * time.Second
	DefaultErrorBackoffMax = 1 * time.Minute
	DefaultLogQueryLimit   = 200
	DefaultNgramLength     = 6
)

// Config holds configuration for the correctness verification service.
type Config struct {
	LokiQueryEndpoints   []string      `yaml:"loki_query_endpoints"`
	TenantID             string        `yaml:"tenant_id"`
	QueryInterval        time.Duration `yaml:"query_interval"`
	QueryIngestersWithin time.Duration `yaml:"-"`
	QueryRangeMin        time.Duration `yaml:"query_range_min"`
	QueryRangeMax        time.Duration `yaml:"query_range_max"`
	MaxLookback          time.Duration `yaml:"max_lookback"`
	RequestTimeout       time.Duration `yaml:"request_timeout"`
	CycleTimeout         time.Duration `yaml:"cycle_timeout"`
	ErrorBackoffMin      time.Duration `yaml:"error_backoff_min"`
	ErrorBackoffMax      time.Duration `yaml:"error_backoff_max"`
	LogQueryLimit        int           `yaml:"log_query_limit"`
	NgramLength          int           `yaml:"-"`
	StartedAt            time.Time     `yaml:"started_at"`
}

// RegisterFlagsWithPrefix registers configuration flags with the given FlagSet under prefix.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}

	f.Var((*csvString)(&c.LokiQueryEndpoints), prefix+".loki-query-endpoints",
		"Comma-separated Loki query endpoint base URLs")
	f.StringVar(&c.TenantID, prefix+".tenant-id", "",
		"Tenant ID to send via X-Scope-OrgID")
	f.DurationVar(&c.QueryInterval, prefix+".query-interval", DefaultQueryInterval,
		"Delay between correctness verification cycles")
	f.DurationVar(&c.QueryRangeMin, prefix+".query-range-min", DefaultQueryRangeMin,
		"Minimum randomized query range duration")
	f.DurationVar(&c.QueryRangeMax, prefix+".query-range-max", DefaultQueryRangeMax,
		"Maximum randomized query range duration")
	f.DurationVar(&c.MaxLookback, prefix+".max-lookback", DefaultMaxLookback,
		"Maximum lookback from now for randomized query ranges. Combined with started_at as max(started_at, now-max_lookback).")
	f.DurationVar(&c.RequestTimeout, prefix+".request-timeout", DefaultRequestTimeout,
		"HTTP timeout for each Loki request")
	f.DurationVar(&c.CycleTimeout, prefix+".cycle-timeout", DefaultCycleTimeout,
		"Timeout for a full correctness cycle")
	f.DurationVar(&c.ErrorBackoffMin, prefix+".error-backoff-min", DefaultErrorBackoffMin,
		"Minimum backoff delay after cycle failures")
	f.DurationVar(&c.ErrorBackoffMax, prefix+".error-backoff-max", DefaultErrorBackoffMax,
		"Maximum backoff delay after cycle failures")
	f.IntVar(&c.LogQueryLimit, prefix+".log-query-limit", DefaultLogQueryLimit,
		"Maximum log lines requested from Loki when selecting a test needle")
	f.Var((*rfc3339Time)(&c.StartedAt), prefix+".started-at",
		"Override the service start time (RFC3339). When set, query ranges may begin at this time instead of now.")
}

// Validate checks constraints and applies defaults for zero-valued fields.
func (c *Config) Validate() error {
	if c.QueryInterval < 0 {
		return fmt.Errorf("query_interval must be non-negative, got %v", c.QueryInterval)
	}
	if c.QueryIngestersWithin < 0 {
		return fmt.Errorf("query_ingesters_within must be non-negative, got %v", c.QueryIngestersWithin)
	}
	if c.QueryRangeMin < 0 {
		return fmt.Errorf("query_range_min must be non-negative, got %v", c.QueryRangeMin)
	}
	if c.QueryRangeMax < 0 {
		return fmt.Errorf("query_range_max must be non-negative, got %v", c.QueryRangeMax)
	}
	if c.MaxLookback < 0 {
		return fmt.Errorf("max_lookback must be non-negative, got %v", c.MaxLookback)
	}
	if c.RequestTimeout < 0 {
		return fmt.Errorf("request_timeout must be non-negative, got %v", c.RequestTimeout)
	}
	if c.CycleTimeout < 0 {
		return fmt.Errorf("cycle_timeout must be non-negative, got %v", c.CycleTimeout)
	}
	if c.ErrorBackoffMin < 0 {
		return fmt.Errorf("error_backoff_min must be non-negative, got %v", c.ErrorBackoffMin)
	}
	if c.ErrorBackoffMax < 0 {
		return fmt.Errorf("error_backoff_max must be non-negative, got %v", c.ErrorBackoffMax)
	}
	if c.LogQueryLimit < 0 {
		return fmt.Errorf("log_query_limit must be non-negative, got %d", c.LogQueryLimit)
	}
	if c.QueryInterval == 0 {
		c.QueryInterval = DefaultQueryInterval
	}
	if c.QueryRangeMin == 0 {
		c.QueryRangeMin = DefaultQueryRangeMin
	}
	if c.QueryRangeMax == 0 {
		c.QueryRangeMax = DefaultQueryRangeMax
	}
	if c.MaxLookback == 0 {
		c.MaxLookback = DefaultMaxLookback
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = DefaultRequestTimeout
	}
	if c.CycleTimeout == 0 {
		c.CycleTimeout = DefaultCycleTimeout
	}
	if c.ErrorBackoffMin == 0 {
		c.ErrorBackoffMin = DefaultErrorBackoffMin
	}
	if c.ErrorBackoffMax == 0 {
		c.ErrorBackoffMax = DefaultErrorBackoffMax
	}
	if c.LogQueryLimit == 0 {
		c.LogQueryLimit = DefaultLogQueryLimit
	}
	if c.NgramLength == 0 {
		c.NgramLength = DefaultNgramLength
	}
	if c.QueryInterval < time.Second {
		return fmt.Errorf("query_interval must be at least 1s, got %v", c.QueryInterval)
	}
	if c.QueryRangeMin <= 0 {
		return fmt.Errorf("query_range_min must be > 0, got %v", c.QueryRangeMin)
	}
	if c.QueryRangeMax < c.QueryRangeMin {
		return fmt.Errorf("query_range_max must be >= query_range_min, got %v < %v", c.QueryRangeMax, c.QueryRangeMin)
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be > 0, got %v", c.RequestTimeout)
	}
	if c.CycleTimeout <= 0 {
		return fmt.Errorf("cycle_timeout must be > 0, got %v", c.CycleTimeout)
	}
	if c.ErrorBackoffMin <= 0 {
		return fmt.Errorf("error_backoff_min must be > 0, got %v", c.ErrorBackoffMin)
	}
	if c.ErrorBackoffMax < c.ErrorBackoffMin {
		return fmt.Errorf("error_backoff_max must be >= error_backoff_min, got %v < %v", c.ErrorBackoffMax, c.ErrorBackoffMin)
	}
	if c.LogQueryLimit <= 0 {
		return fmt.Errorf("log_query_limit must be > 0, got %d", c.LogQueryLimit)
	}
	if c.NgramLength < 1 || c.NgramLength > 8 {
		return fmt.Errorf("ngram_length must be between 1 and 8, got %d", c.NgramLength)
	}

	if len(c.LokiQueryEndpoints) == 0 {
		return fmt.Errorf("loki_query_endpoints is required")
	}

	c.LokiQueryEndpoints = normalizeEndpoints(c.LokiQueryEndpoints)
	if len(c.LokiQueryEndpoints) == 0 {
		return fmt.Errorf("loki_query_endpoints is required")
	}

	if c.QueryIngestersWithin == 0 {
		return fmt.Errorf("query_ingesters_within must be set (or inherited from Loki's querier.query-ingesters-within)")
	}

	for _, endpoint := range c.LokiQueryEndpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("invalid Loki endpoint %q: %w", endpoint, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("invalid Loki endpoint %q: scheme must be http or https", endpoint)
		}
		if u.Host == "" {
			return fmt.Errorf("invalid Loki endpoint %q: host is required", endpoint)
		}
	}

	return nil
}

func normalizeEndpoints(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, endpoint := range in {
		trimmed := strings.TrimSpace(endpoint)
		trimmed = strings.TrimRight(trimmed, "/")
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// ParseEndpointCSV parses a comma-separated list of endpoints.
func ParseEndpointCSV(value string) []string {
	if value == "" {
		return nil
	}
	return normalizeEndpoints(strings.Split(value, ","))
}

type csvString []string

func (c *csvString) String() string {
	if c == nil {
		return ""
	}
	return strings.Join(*c, ",")
}

func (c *csvString) Set(value string) error {
	*c = ParseEndpointCSV(value)
	return nil
}

// rfc3339Time implements flag.Value for RFC3339 timestamps.
type rfc3339Time time.Time

func (t *rfc3339Time) String() string {
	if t == nil || (*time.Time)(t).IsZero() {
		return ""
	}
	return (*time.Time)(t).Format(time.RFC3339)
}

func (t *rfc3339Time) Set(value string) error {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("invalid RFC3339 time %q: %w", value, err)
	}
	*t = rfc3339Time(parsed.UTC())
	return nil
}
