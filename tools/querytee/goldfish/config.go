package goldfish

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

const (
	// ResultsPersistenceModeMismatchOnly persists only mismatched comparisons.
	ResultsPersistenceModeMismatchOnly ResultsPersistenceMode = "mismatch-only"
	// ResultsPersistenceModeAll persists all sampled comparisons regardless of outcome.
	ResultsPersistenceModeAll ResultsPersistenceMode = "all"

	// ResultsBackendGCS stores results in Google Cloud Storage.
	ResultsBackendGCS = bucket.GCS
	// ResultsBackendS3 stores results in Amazon S3.
	ResultsBackendS3 = bucket.S3

	// ResultsCompressionNone stores payloads without additional compression.
	ResultsCompressionNone = "none"
	// ResultsCompressionGzip gzip-compresses payloads before upload.
	ResultsCompressionGzip = "gzip"

	defaultResultsObjectPrefix = "goldfish/results"
)

// ResultsPersistenceMode describes how often to persist query results.
type ResultsPersistenceMode string

// Config holds the Goldfish configuration
type Config struct {
	Enabled bool `yaml:"enabled"`

	// Sampling configuration
	SamplingConfig SamplingConfig `yaml:"sampling"`

	// Storage configuration (SQL backend for metadata)
	StorageConfig StorageConfig `yaml:"storage"`

	// Result storage configuration (object storage for raw payloads)
	ResultsStorage ResultsStorageConfig `yaml:"results"`

	// Performance comparison tolerance (0.0-1.0, where 0.1 = 10%)
	PerformanceTolerance float64 `yaml:"performance_tolerance"`

	// ComparisonMinAge is the minimum age of data to send to goldfish for comparison.
	// Only data older than this threshold will be compared. Data newer than this threshold
	// will only be fetched from the main cell. When set to 0 (default), all data is compared.
	ComparisonMinAge time.Duration `yaml:"comparison_min_age" category:"experimental"`

	// ComparisonStartDate is the start data of data we want to compare with goldfish.
	// This is part of the query splitting feature. Queries will be split into up to three parts:
	// 1. Data before ComparisonStartDate -> split goes to the preferred backend only
	// 2. Data between ComparisonStartDate and (now - ComparisonMinAge) -> splits go to all backends and are compared with goldfish
	// 3. Data after (now - ComparisonMinAge) -> split goes to the preferred backend only
	ComparisonStartDate flagext.Time `yaml:"storage_start_date" category:"experimental"`
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

// ResultsStorageConfig defines configuration for storing raw query results in object storage.
type ResultsStorageConfig struct {
	Enabled bool `yaml:"enabled"`

	// Mode controls when to persist query results (mismatch-only or all).
	Mode ResultsPersistenceMode `yaml:"mode"`

	// Backend selects the object storage provider (gcs, s3). When omitted we attempt to infer from SQL storage type.
	Backend string `yaml:"backend"`

	// ObjectPrefix is appended to generated object keys inside the bucket.
	ObjectPrefix string `yaml:"object_prefix"`

	// Compression codec applied before upload (gzip or none)
	Compression string `yaml:"compression"`

	// Bucket holds provider-specific configuration exposed through the shared bucket client.
	Bucket bucket.Config `yaml:"bucket"`
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

	// Result storage flags
	f.BoolVar(&cfg.ResultsStorage.Enabled, "goldfish.results.enabled", false, "Enable persisting raw query results to object storage")
	f.StringVar((*string)(&cfg.ResultsStorage.Mode), "goldfish.results.mode", string(ResultsPersistenceModeMismatchOnly), "Result persistence mode (mismatch-only or all)")
	f.StringVar(&cfg.ResultsStorage.Backend, "goldfish.results.backend", "", "Results storage backend (gcs, s3). When empty, inferred from goldfish.storage.type")
	f.StringVar(&cfg.ResultsStorage.ObjectPrefix, "goldfish.results.prefix", defaultResultsObjectPrefix, "Prefix for objects stored in the bucket")
	f.StringVar(&cfg.ResultsStorage.Compression, "goldfish.results.compression", ResultsCompressionGzip, "Compression codec for stored results (gzip or none)")
	cfg.ResultsStorage.Bucket.RegisterFlagsWithPrefix("goldfish.results.", f)

	// Performance comparison flags
	f.Float64Var(&cfg.PerformanceTolerance, "goldfish.performance-tolerance", 0.1, "Performance comparison tolerance (0.0-1.0, where 0.1 = 10%)")

	// Query splitting flags
	f.DurationVar(&cfg.ComparisonMinAge, "goldfish.comparison-min-age", 0, "Minimum age of data to compare with goldfish. Only data older than this threshold is sent to goldfish for comparison. When 0 (default), query splitting is disabled and all data is compared.")
	f.Var(&cfg.ComparisonStartDate, "goldfish.comparison-start-date", "Initial date when data objects became available. Format YYYY-MM-DD. If not set, assume data objects are always available and splits for this time range will go to all backends. If set, splits for data older than this date will go to the preferred backend only.")
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

	// Validate comparison min age (0 disables splitting, otherwise must be positive)
	if cfg.ComparisonMinAge < 0 {
		return errors.New("comparison min age must be >= 0 (0 disables query splitting)")
	}

	// Validate SQL storage if configured
	if err := cfg.StorageConfig.validate(); err != nil {
		return err
	}

	// Validate result storage when enabled
	if cfg.ResultsStorage.Enabled {
		if cfg.StorageConfig.Type == "" {
			return errors.New("goldfish.results.enabled requires a SQL storage backend (cloudsql or rds)")
		}
		if err := cfg.ResultsStorage.validate(cfg.StorageConfig.Type); err != nil {
			return err
		}
	}

	return nil
}

func (cfg StorageConfig) validate() error {
	if cfg.Type == "" {
		return nil
	}

	switch cfg.Type {
	case "cloudsql":
		if cfg.CloudSQLDatabase == "" || cfg.CloudSQLUser == "" {
			return errors.New("CloudSQL database and user must be specified")
		}
	case "rds":
		if cfg.RDSEndpoint == "" || cfg.RDSDatabase == "" || cfg.RDSUser == "" {
			return errors.New("RDS endpoint, database, and user must be specified")
		}
	default:
		return fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}

	return nil
}

func (cfg *ResultsStorageConfig) validate(sqlStorageType string) error {
	mode := strings.ToLower(strings.TrimSpace(string(cfg.Mode)))
	if mode == "" {
		cfg.Mode = ResultsPersistenceModeMismatchOnly
	} else {
		cfg.Mode = ResultsPersistenceMode(mode)
	}

	switch cfg.Mode {
	case ResultsPersistenceModeMismatchOnly, ResultsPersistenceModeAll:
	default:
		return fmt.Errorf("unsupported goldfish.results.mode: %s", cfg.Mode)
	}

	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = inferResultsBackend(sqlStorageType)
		cfg.Backend = backend
	}

	switch backend {
	case ResultsBackendGCS, ResultsBackendS3:
	case "":
		return errors.New("goldfish.results.backend must be specified when goldfish.results.enabled is true")
	default:
		return fmt.Errorf("unsupported goldfish.results.backend: %s", cfg.Backend)
	}

	if cfg.ObjectPrefix == "" {
		cfg.ObjectPrefix = defaultResultsObjectPrefix
	}

	compression := strings.ToLower(strings.TrimSpace(cfg.Compression))
	if compression == "" {
		compression = ResultsCompressionGzip
	}
	switch compression {
	case ResultsCompressionGzip, ResultsCompressionNone:
		cfg.Compression = compression
	default:
		return fmt.Errorf("unsupported goldfish.results.compression: %s", cfg.Compression)
	}

	if err := cfg.Bucket.Validate(); err != nil {
		return fmt.Errorf("invalid goldfish.results.bucket configuration: %w", err)
	}

	return nil
}

func inferResultsBackend(sqlStorageType string) string {
	switch sqlStorageType {
	case "cloudsql":
		return ResultsBackendGCS
	case "rds":
		return ResultsBackendS3
	default:
		return ""
	}
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

	pairs := strings.SplitSeq(value, ",")
	for pair := range pairs {
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
