package engine

import (
	"flag"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/netutil"

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

	EnableEngineRouter       bool   `yaml:"enable_engine_router" category:"experimental"`
	DownstreamAddress        string `yaml:"downstream_address" category:"experimental"`
	EnableDeleteReqFiltering bool   `yaml:"enable_delete_req_filtering" category:"experimental"`
	EnforceRetentionPeriod   bool   `yaml:"enforce_retention_period" category:"experimental"`

	// EnforceQuerySeriesLimit enables enforcement of the max_query_series limit.
	// When enabled, the tenant's MaxQuerySeries limit is applied; otherwise, no limit is enforced.
	EnforceQuerySeriesLimit bool `yaml:"enforce_max_query_series_limit" category:"experimental"`
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

	f.BoolVar(&cfg.EnableEngineRouter, prefix+"enable-engine-router", false, "Enable routing of query splits in the query frontend to the next generation engine when they fall within the configured time range.")
	f.StringVar(&cfg.DownstreamAddress, prefix+"downstream-address", "", "Downstream address to send query splits to. This is the HTTP handler address of the query engine scheduler.")
	f.BoolVar(&cfg.EnforceRetentionPeriod, prefix+"enforce-retention-period", false, "Enforce tenant retention limits. Queries falling outside tenant's retention period are either adjusted or rejected.")
	f.BoolVar(&cfg.EnableDeleteReqFiltering, prefix+"enable-delete-req-filtering", true, "When enabled, query results exclude log lines that match overlapping delete requests (not just pending requests). Disable to return all logs without considering delete requests.")
	f.BoolVar(&cfg.EnforceQuerySeriesLimit, prefix+"enforce-max-query-series-limit", false, "Experimental: When enabled, the tenant's MaxQuerySeries limit is applied. Otherwise, no limit is enforced.")
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
