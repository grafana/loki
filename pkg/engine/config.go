package engine

import (
	"flag"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/netutil"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// Config holds the configuration options to use with the next generation Loki
// Query Engine.
type Config struct {
	// Enable the next generation Loki Query Engine for supported queries.
	Enable      bool `yaml:"enable" category:"experimental"`
	Distributed bool `yaml:"distributed" category:"experimental"`

	// InterfaceNames specifies the list of network interfaces to use for
	// accepting incoming traffic. The public address of the instance is
	// inferred from the interfaces in this list.
	InterfaceNames []string `yaml:"instance_interface_names" doc:"default=[<private network interfaces>]" category:"experimental"`

	Executor ExecutorConfig `yaml:",inline"`
	Worker   WorkerConfig   `yaml:",inline"`

	StorageLag           time.Duration `yaml:"storage_lag" category:"experimental"`
	StorageStartDate     flagext.Time  `yaml:"storage_start_date" category:"experimental"`
	StorageRetentionDays int64         `yaml:"storage_retention_days" category:"experimental"`

	// V1OnlyStreamSelector is a LogQL stream selector. Queries whose stream
	// selector contains all of its matchers are always executed by the v1
	// (chunks) engine, regardless of the configured storage time range.
	V1OnlyStreamSelector string `yaml:"v1_only_stream_selector" category:"experimental"`
	// V1OnlyMatchers is parsed from V1OnlyStreamSelector during validation.
	V1OnlyMatchers []*labels.Matcher `yaml:"-" json:"-"`

	EnableEngineRouter       bool   `yaml:"enable_engine_router" category:"experimental"`
	DownstreamAddress        string `yaml:"downstream_address" category:"experimental"`
	EnableDeleteReqFiltering bool   `yaml:"enable_delete_req_filtering" category:"experimental"`
	EnforceRetentionPeriod   bool   `yaml:"enforce_retention_period" category:"experimental"`

	// Mutate incoming queries to align their start and end with their step.
	AlignQueriesWithStep bool `yaml:"align_queries_with_step" category:"experimental"`

	// EnforceQuerySeriesLimit enables enforcement of the max_query_series limit.
	// When enabled, the tenant's MaxQuerySeries limit is applied; otherwise, no limit is enforced.
	EnforceQuerySeriesLimit bool `yaml:"enforce_max_query_series_limit" category:"experimental"`

	ResultsCache resultscache.Config `yaml:"results_cache" category:"experimental"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("query-engine.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enable, prefix+"enable", false, "Experimental: Enable next generation query engine for supported queries.")
	f.BoolVar(&cfg.Distributed, prefix+"distributed", false, "Experimental: Enable distributed query execution.")

	cfg.InterfaceNames = netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
	f.Var((*flagext.StringSlice)(&cfg.InterfaceNames), prefix+"instance-interface-names", "Experimental: Name of network interface to read an advertise address from for accepting incoming traffic from query-engine-worker instances when distributed execution is enabled.")

	cfg.Executor.RegisterFlagsWithPrefix(prefix, f)
	cfg.Worker.RegisterFlagsWithPrefix(prefix, f)

	f.DurationVar(&cfg.StorageLag, prefix+"storage-lag", 1*time.Hour, "Amount of time until data objects are available.")
	f.Var(&cfg.StorageStartDate, prefix+"storage-start-date", "Initial date when data objects became available. Format YYYY-MM-DD. If not set, assume data objects are always available no matter how far back.")
	f.Int64Var(&cfg.StorageRetentionDays, prefix+"storage-retention-days", 0, "Lifecycle of data objects in days. If set, queries falling outside of the retention period will not be supported. When both storage-start-date and storage-retention-days are set, the more restrictive of the two will apply.")
	f.StringVar(&cfg.V1OnlyStreamSelector, prefix+"v1-only-stream-selector", "", "Experimental: LogQL stream selector, e.g. '{app=\"foo\"}'. Queries whose stream selector contains all of these matchers are always executed by the v1 (chunks) engine. Only equality matchers are allowed, and only queries using the exact same equality matchers are detected. Empty disables the feature.")

	f.BoolVar(&cfg.EnableEngineRouter, prefix+"enable-engine-router", false, "Enable routing of query splits in the query frontend to the next generation engine when they fall within the configured time range.")
	f.StringVar(&cfg.DownstreamAddress, prefix+"downstream-address", "", "Downstream address to send query splits to. This is the HTTP handler address of the query engine scheduler.")
	f.BoolVar(&cfg.EnforceRetentionPeriod, prefix+"enforce-retention-period", false, "Enforce tenant retention limits. Queries falling outside tenant's retention period are either adjusted or rejected.")
	f.BoolVar(&cfg.EnableDeleteReqFiltering, prefix+"enable-delete-req-filtering", true, "When enabled, query results exclude log lines that match overlapping delete requests (not just pending requests). Disable to return all logs without considering delete requests.")
	f.BoolVar(&cfg.AlignQueriesWithStep, prefix+"align-queries-with-step", false, "Mutate incoming queries to align their start and end with their step.")
	f.BoolVar(&cfg.EnforceQuerySeriesLimit, prefix+"enforce-max-query-series-limit", false, "Experimental: When enabled, the tenant's MaxQuerySeries limit is applied. Otherwise, no limit is enforced.")
	cfg.ResultsCache.RegisterFlagsWithPrefix(f, prefix+"results-cache.")
}

// Validate validates the config and populates derived fields such as
// V1OnlyMatchers. It is idempotent.
func (cfg *Config) Validate() error {
	if cfg.V1OnlyStreamSelector == "" {
		cfg.V1OnlyMatchers = nil
		return nil
	}

	matchers, err := ParseV1OnlySelector(cfg.V1OnlyStreamSelector)
	if err != nil {
		return err
	}
	cfg.V1OnlyMatchers = matchers
	return nil
}

// ParseV1OnlySelector parses a v1-only stream selector into label matchers.
// Only equality matchers are allowed: matching against queries is done by
// exact matcher equality, so a regex or negative matcher in the selector
// would silently never match.
func ParseV1OnlySelector(selector string) ([]*labels.Matcher, error) {
	matchers, err := syntax.ParseMatchers(selector, true)
	if err != nil {
		return nil, fmt.Errorf("invalid v1-only stream selector %q: %w", selector, err)
	}
	for _, m := range matchers {
		if m.Type != labels.MatchEqual {
			return nil, fmt.Errorf("v1-only stream selector must contain only equality matchers, got %s", m)
		}
	}
	return matchers, nil
}

// MatchesV1OnlySelector returns true if any of the query's matcher groups
// contains all of the configured V1OnlyMatchers, compared by exact matcher
// equality. Such queries must be executed by the v1 (chunks) engine.
func (cfg *Config) MatchesV1OnlySelector(params logql.Params) bool {
	if len(cfg.V1OnlyMatchers) == 0 {
		return false
	}

	expr := params.GetExpression()
	if expr == nil {
		return false
	}

	groups, err := syntax.MatcherGroups(expr)
	if err != nil {
		return false
	}

	for _, group := range groups {
		if containsAllMatchers(group.Matchers, cfg.V1OnlyMatchers) {
			return true
		}
	}
	return false
}

// containsAllMatchers returns true if every matcher in required has an exact
// equal (name, type, value) in group.
func containsAllMatchers(group, required []*labels.Matcher) bool {
	for _, req := range required {
		found := false
		for _, m := range group {
			if m.Type == req.Type && m.Name == req.Name && m.Value == req.Value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (cfg *Config) ValidQueryRange() (time.Time, time.Time) {
	startDate := time.Time(cfg.StorageStartDate).UTC()
	now := time.Now().UTC()

	if cfg.StorageRetentionDays > 0 {
		// considering start of the day for retention calculations.
		// e.g. if retention is 7 days, and today is 10th, data before 3rd is not available.
		retentionBoundary := now.Truncate(24 * time.Hour).
			Add(-time.Duration(cfg.StorageRetentionDays) * 24 * time.Hour)

		if startDate.IsZero() || retentionBoundary.After(startDate) {
			startDate = retentionBoundary
		}
	}

	return startDate, now.Add(-cfg.StorageLag)
}

// AdvertiseAddress determines the TCP address to advertise for accepting
// incoming traffic from workers. Returns nil, nil if distributed execution is
// not enabled.
//
// The provided listenPort is used to construct the TCP address to advertise.
func (cfg *Config) AdvertiseAddr(listenPort uint16) (*net.TCPAddr, error) {
	if !cfg.Distributed {
		return nil, nil
	}

	// TODO(rfratto): Should IPv6 be configurable?
	addr, err := netutil.GetFirstAddressOf(cfg.InterfaceNames, util_log.Logger, false)
	if err != nil {
		return nil, err
	}

	parsedAddr, err := netip.ParseAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("parsing discovered address %s: %w", addr, err)
	}
	return net.TCPAddrFromAddrPort(netip.AddrPortFrom(parsedAddr, listenPort)), nil
}
