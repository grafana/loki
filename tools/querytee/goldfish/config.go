package goldfish

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// Config holds the Goldfish configuration
type Config struct {
	Enabled bool `yaml:"enabled"`

	// Sampling configuration
	SamplingConfig SamplingConfig `yaml:"sampling"`

	// Storage configuration
	StorageConfig StorageConfig `yaml:"storage"`

	// Performance comparison tolerance (0.0-1.0, where 0.1 = 10%)
	PerformanceTolerance float64 `yaml:"performance_tolerance"`
}

// SamplingConfig defines how queries are sampled
type SamplingConfig struct {
	DefaultRate float64            `yaml:"default_rate"`
	TenantRules map[string]float64 `yaml:"tenant_rules"`
}

// StorageConfig defines storage backend configuration
type StorageConfig struct {
	Type string `yaml:"type"` // "cloudsql", "rds", or empty string for no storage

	// CloudSQL specific (via proxy)
	CloudSQLHost     string `yaml:"cloudsql_host"`
	CloudSQLPort     int    `yaml:"cloudsql_port"`
	CloudSQLDatabase string `yaml:"cloudsql_database"`
	CloudSQLUser     string `yaml:"cloudsql_user"`
	// CloudSQLPassword provided via GOLDFISH_DB_PASSWORD environment variable

	// RDS specific
	RDSEndpoint string `yaml:"rds_endpoint"` // e.g., "mydb.123456789012.us-east-1.rds.amazonaws.com:3306"
	RDSDatabase string `yaml:"rds_database"`
	RDSUser     string `yaml:"rds_user"`
	// RDSPassword provided via GOLDFISH_DB_PASSWORD environment variable

	// Common settings
	MaxConnections int `yaml:"max_connections"`
	MaxIdleTime    int `yaml:"max_idle_time_seconds"`
}

// RegisterFlags registers Goldfish flags
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "goldfish.enabled", false, "Enable Goldfish query sampling and comparison")

	// Sampling flags
	f.Float64Var(&cfg.SamplingConfig.DefaultRate, "goldfish.sampling.default-rate", 0.0, "Default sampling rate (0.0-1.0)")
	f.Var(&tenantRulesFlag{&cfg.SamplingConfig.TenantRules}, "goldfish.sampling.tenant-rules", "Tenant-specific sampling rules (format: tenant1:0.1,tenant2:0.5)")

	// Storage flags
	f.StringVar(&cfg.StorageConfig.Type, "goldfish.storage.type", "", "Storage backend type (cloudsql, rds, or empty for no storage)")

	// CloudSQL flags
	f.StringVar(&cfg.StorageConfig.CloudSQLHost, "goldfish.storage.cloudsql.host", "cloudsql-proxy", "CloudSQL proxy host")
	f.IntVar(&cfg.StorageConfig.CloudSQLPort, "goldfish.storage.cloudsql.port", 3306, "CloudSQL proxy port")
	f.StringVar(&cfg.StorageConfig.CloudSQLDatabase, "goldfish.storage.cloudsql.database", "", "CloudSQL database name")
	f.StringVar(&cfg.StorageConfig.CloudSQLUser, "goldfish.storage.cloudsql.user", "", "CloudSQL database user")

	// RDS flags
	f.StringVar(&cfg.StorageConfig.RDSEndpoint, "goldfish.storage.rds.endpoint", "", "RDS endpoint (host:port)")
	f.StringVar(&cfg.StorageConfig.RDSDatabase, "goldfish.storage.rds.database", "", "RDS database name")
	f.StringVar(&cfg.StorageConfig.RDSUser, "goldfish.storage.rds.user", "", "RDS database user")
	f.IntVar(&cfg.StorageConfig.MaxConnections, "goldfish.storage.max-connections", 10, "Maximum database connections")
	f.IntVar(&cfg.StorageConfig.MaxIdleTime, "goldfish.storage.max-idle-time", 300, "Maximum idle time in seconds")

	// Performance comparison flags
	f.Float64Var(&cfg.PerformanceTolerance, "goldfish.performance-tolerance", 0.1, "Performance comparison tolerance (0.0-1.0, where 0.1 = 10%)")
}

// Validate validates the configuration
func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.SamplingConfig.DefaultRate < 0 || cfg.SamplingConfig.DefaultRate > 1 {
		return errors.New("default sampling rate must be between 0 and 1")
	}

	for tenant, rate := range cfg.SamplingConfig.TenantRules {
		if rate < 0 || rate > 1 {
			return fmt.Errorf("sampling rate for tenant %s must be between 0 and 1", tenant)
		}
	}

	if cfg.PerformanceTolerance < 0 || cfg.PerformanceTolerance > 1 {
		return errors.New("performance tolerance must be between 0 and 1")
	}

	// Only validate storage if one is configured
	if cfg.StorageConfig.Type == "" {
		return nil
	}

	switch cfg.StorageConfig.Type {
	case "cloudsql":
		if cfg.StorageConfig.CloudSQLDatabase == "" || cfg.StorageConfig.CloudSQLUser == "" {
			return errors.New("CloudSQL database and user must be specified")
		}
	case "rds":
		if cfg.StorageConfig.RDSEndpoint == "" || cfg.StorageConfig.RDSDatabase == "" || cfg.StorageConfig.RDSUser == "" {
			return errors.New("RDS endpoint, database, and user must be specified")
		}
	default:
		return fmt.Errorf("unsupported storage type: %s", cfg.StorageConfig.Type)
	}

	return nil
}

// tenantRulesFlag implements flag.Value for parsing tenant rules
type tenantRulesFlag struct {
	target *map[string]float64
}

func (f *tenantRulesFlag) String() string {
	if f.target == nil || *f.target == nil {
		return ""
	}
	var parts []string
	for tenant, rate := range *f.target {
		parts = append(parts, fmt.Sprintf("%s:%g", tenant, rate))
	}
	return strings.Join(parts, ",")
}

func (f *tenantRulesFlag) Set(value string) error {
	if *f.target == nil {
		*f.target = make(map[string]float64)
	}

	if value == "" {
		return nil
	}

	pairs := strings.Split(value, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid tenant rule format: %s", pair)
		}

		tenant := strings.TrimSpace(parts[0])
		rateStr := strings.TrimSpace(parts[1])

		rate, err := parseRate(rateStr)
		if err != nil {
			return fmt.Errorf("invalid rate for tenant %s: %w", tenant, err)
		}

		(*f.target)[tenant] = rate
	}

	return nil
}

// parseRate parses a rate string that can be decimal (0.1) or percentage (10%)
func parseRate(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		percentStr := strings.TrimSuffix(s, "%")
		percent, err := strconv.ParseFloat(percentStr, 64)
		if err != nil {
			return 0, err
		}
		return percent / 100.0, nil
	}

	return strconv.ParseFloat(s, 64)
}
