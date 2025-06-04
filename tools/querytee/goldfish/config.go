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
}

// SamplingConfig defines how queries are sampled
type SamplingConfig struct {
	DefaultRate float64            `yaml:"default_rate"`
	TenantRules map[string]float64 `yaml:"tenant_rules"`
}

// StorageConfig defines storage backend configuration
type StorageConfig struct {
	Type string `yaml:"type"` // "cloudsql", "bigquery", "rds", etc.

	// CloudSQL specific (via proxy)
	CloudSQLHost     string `yaml:"cloudsql_host"`
	CloudSQLPort     int    `yaml:"cloudsql_port"`
	CloudSQLDatabase string `yaml:"cloudsql_database"`
	CloudSQLUser     string `yaml:"cloudsql_user"`
	CloudSQLPassword string `yaml:"cloudsql_password"`

	// BigQuery specific
	BigQueryProject string `yaml:"bigquery_project"`
	BigQueryDataset string `yaml:"bigquery_dataset"`

	// Generic SQL
	DSN string `yaml:"dsn"`

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
	f.StringVar(&cfg.StorageConfig.Type, "goldfish.storage.type", "", "Storage backend type (cloudsql, bigquery, rds)")
	f.StringVar(&cfg.StorageConfig.CloudSQLHost, "goldfish.storage.cloudsql.host", "cloudsql-proxy", "CloudSQL proxy host")
	f.IntVar(&cfg.StorageConfig.CloudSQLPort, "goldfish.storage.cloudsql.port", 5432, "CloudSQL proxy port")
	f.StringVar(&cfg.StorageConfig.CloudSQLDatabase, "goldfish.storage.cloudsql.database", "", "CloudSQL database name")
	f.StringVar(&cfg.StorageConfig.CloudSQLUser, "goldfish.storage.cloudsql.user", "", "CloudSQL database user")
	f.StringVar(&cfg.StorageConfig.CloudSQLPassword, "goldfish.storage.cloudsql.password", "", "CloudSQL database password")
	f.StringVar(&cfg.StorageConfig.BigQueryProject, "goldfish.storage.bigquery.project", "", "BigQuery project ID")
	f.StringVar(&cfg.StorageConfig.BigQueryDataset, "goldfish.storage.bigquery.dataset", "", "BigQuery dataset name")
	f.StringVar(&cfg.StorageConfig.DSN, "goldfish.storage.dsn", "", "Generic database DSN")
	f.IntVar(&cfg.StorageConfig.MaxConnections, "goldfish.storage.max-connections", 10, "Maximum database connections")
	f.IntVar(&cfg.StorageConfig.MaxIdleTime, "goldfish.storage.max-idle-time", 300, "Maximum idle time in seconds")
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

	// Only validate storage if one is configured
	if cfg.StorageConfig.Type == "" {
		return nil
	}

	switch cfg.StorageConfig.Type {
	case "cloudsql":
		if cfg.StorageConfig.CloudSQLDatabase == "" || cfg.StorageConfig.CloudSQLUser == "" {
			return errors.New("CloudSQL database and user must be specified")
		}
	case "bigquery":
		if cfg.StorageConfig.BigQueryProject == "" || cfg.StorageConfig.BigQueryDataset == "" {
			return errors.New("BigQuery project and dataset must be specified")
		}
	case "postgres", "mysql":
		if cfg.StorageConfig.DSN == "" {
			return errors.New("DSN must be specified for generic SQL storage")
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
