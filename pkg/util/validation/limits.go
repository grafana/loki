package validation

import (
	"flag"
	"time"

	"github.com/grafana/loki/pkg/util/flagext"
)

const (
	// Local ingestion rate strategy
	LocalIngestionRateStrategy = "local"

	// Global ingestion rate strategy
	GlobalIngestionRateStrategy = "global"

	bytesInMB = 1048576
)

// Limits describe all the limits for users; can be used to describe global default
// limits via flags, or per-user limits via yaml config.
type Limits struct {
	// Distributor enforced limits.
	IngestionRateStrategy  string           `yaml:"ingestion_rate_strategy"`
	IngestionRateMB        float64          `yaml:"ingestion_rate_mb"`
	IngestionBurstSizeMB   float64          `yaml:"ingestion_burst_size_mb"`
	MaxLabelNameLength     int              `yaml:"max_label_name_length"`
	MaxLabelValueLength    int              `yaml:"max_label_value_length"`
	MaxLabelNamesPerSeries int              `yaml:"max_label_names_per_series"`
	RejectOldSamples       bool             `yaml:"reject_old_samples"`
	RejectOldSamplesMaxAge time.Duration    `yaml:"reject_old_samples_max_age"`
	CreationGracePeriod    time.Duration    `yaml:"creation_grace_period"`
	EnforceMetricName      bool             `yaml:"enforce_metric_name"`
	MaxLineSize            flagext.ByteSize `yaml:"max_line_size"`

	// Ingester enforced limits.
	MaxLocalStreamsPerUser  int `yaml:"max_streams_per_user"`
	MaxGlobalStreamsPerUser int `yaml:"max_global_streams_per_user"`

	// Querier enforced limits.
	MaxChunksPerQuery          int           `yaml:"max_chunks_per_query"`
	MaxQuerySeries             int           `yaml:"max_query_series"`
	MaxQueryLookback           time.Duration `yaml:"max_query_lookback"`
	MaxQueryLength             time.Duration `yaml:"max_query_length"`
	MaxQueryParallelism        int           `yaml:"max_query_parallelism"`
	CardinalityLimit           int           `yaml:"cardinality_limit"`
	MaxStreamsMatchersPerQuery int           `yaml:"max_streams_matchers_per_query"`
	MaxConcurrentTailRequests  int           `yaml:"max_concurrent_tail_requests"`
	MaxEntriesLimitPerQuery    int           `yaml:"max_entries_limit_per_query"`
	MaxCacheFreshness          time.Duration `yaml:"max_cache_freshness_per_query"`

	// Query frontend enforced limits. The default is actually parameterized by the queryrange config.
	QuerySplitDuration time.Duration `yaml:"split_queries_by_interval"`

	// Config for overrides, convenient if it goes here.
	PerTenantOverrideConfig string        `yaml:"per_tenant_override_config"`
	PerTenantOverridePeriod time.Duration `yaml:"per_tenant_override_period"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (l *Limits) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&l.IngestionRateStrategy, "distributor.ingestion-rate-limit-strategy", "local", "Whether the ingestion rate limit should be applied individually to each distributor instance (local), or evenly shared across the cluster (global).")
	f.Float64Var(&l.IngestionRateMB, "distributor.ingestion-rate-limit-mb", 4, "Per-user ingestion rate limit in sample size per second. Units in MB.")
	f.Float64Var(&l.IngestionBurstSizeMB, "distributor.ingestion-burst-size-mb", 6, "Per-user allowed ingestion burst size (in sample size). Units in MB.")
	f.Var(&l.MaxLineSize, "distributor.max-line-size", "maximum line length allowed, i.e. 100mb. Default (0) means unlimited.")
	f.IntVar(&l.MaxLabelNameLength, "validation.max-length-label-name", 1024, "Maximum length accepted for label names")
	f.IntVar(&l.MaxLabelValueLength, "validation.max-length-label-value", 2048, "Maximum length accepted for label value. This setting also applies to the metric name")
	f.IntVar(&l.MaxLabelNamesPerSeries, "validation.max-label-names-per-series", 30, "Maximum number of label names per series.")
	f.BoolVar(&l.RejectOldSamples, "validation.reject-old-samples", false, "Reject old samples.")
	f.DurationVar(&l.RejectOldSamplesMaxAge, "validation.reject-old-samples.max-age", 14*24*time.Hour, "Maximum accepted sample age before rejecting.")
	f.DurationVar(&l.CreationGracePeriod, "validation.create-grace-period", 10*time.Minute, "Duration which table will be created/deleted before/after it's needed; we won't accept sample from before this time.")
	f.BoolVar(&l.EnforceMetricName, "validation.enforce-metric-name", true, "Enforce every sample has a metric name.")
	f.IntVar(&l.MaxEntriesLimitPerQuery, "validation.max-entries-limit", 5000, "Per-user entries limit per query")

	f.IntVar(&l.MaxLocalStreamsPerUser, "ingester.max-streams-per-user", 10e3, "Maximum number of active streams per user, per ingester. 0 to disable.")
	f.IntVar(&l.MaxGlobalStreamsPerUser, "ingester.max-global-streams-per-user", 0, "Maximum number of active streams per user, across the cluster. 0 to disable.")

	f.IntVar(&l.MaxChunksPerQuery, "store.query-chunk-limit", 2e6, "Maximum number of chunks that can be fetched in a single query.")
	f.DurationVar(&l.MaxQueryLength, "store.max-query-length", 0, "Limit to length of chunk store queries, 0 to disable.")
	f.IntVar(&l.MaxQuerySeries, "querier.max-query-series", 500, "Limit the maximum of unique series returned by a metric query. When the limit is reached an error is returned.")
	f.DurationVar(&l.MaxQueryLookback, "querier.max-query-lookback", 0, "Limit how long back data (series and metadata) can be queried, up until <lookback> duration ago. This limit is enforced in the query-frontend, querier and ruler. If the requested time range is outside the allowed range, the request will not fail but will be manipulated to only query data within the allowed time range. 0 to disable.")
	f.IntVar(&l.MaxQueryParallelism, "querier.max-query-parallelism", 14, "Maximum number of queries will be scheduled in parallel by the frontend.")
	f.IntVar(&l.CardinalityLimit, "store.cardinality-limit", 1e5, "Cardinality limit for index queries.")
	f.IntVar(&l.MaxStreamsMatchersPerQuery, "querier.max-streams-matcher-per-query", 1000, "Limit the number of streams matchers per query")
	f.IntVar(&l.MaxConcurrentTailRequests, "querier.max-concurrent-tail-requests", 10, "Limit the number of concurrent tail requests")
	f.DurationVar(&l.MaxCacheFreshness, "frontend.max-cache-freshness", 1*time.Minute, "Most recent allowed cacheable result per-tenant, to prevent caching very recent results that might still be in flux.")

	f.StringVar(&l.PerTenantOverrideConfig, "limits.per-user-override-config", "", "File name of per-user overrides.")
	f.DurationVar(&l.PerTenantOverridePeriod, "limits.per-user-override-period", 10*time.Second, "Period with this to reload the overrides.")
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (l *Limits) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.  See prometheus/config.

	// During startup we wont have a default value so we don't want to overwrite them
	if defaultLimits != nil {
		*l = *defaultLimits
	}
	type plain Limits
	return unmarshal((*plain)(l))
}

// When we load YAML from disk, we want the various per-customer limits
// to default to any values specified on the command line, not default
// command line values.  This global contains those values.  I (Tom) cannot
// find a nicer way I'm afraid.
var defaultLimits *Limits

// SetDefaultLimitsForYAMLUnmarshalling sets global default limits, used when loading
// Limits from YAML files. This is used to ensure per-tenant limits are defaulted to
// those values.
func SetDefaultLimitsForYAMLUnmarshalling(defaults Limits) {
	defaultLimits = &defaults
}

// TenantLimits is a function that returns limits for given tenant, or
// nil, if there are no tenant-specific limits.
type TenantLimits func(userID string) *Limits

// Overrides periodically fetch a set of per-user overrides, and provides convenience
// functions for fetching the correct value.
type Overrides struct {
	defaultLimits *Limits
	tenantLimits  TenantLimits
}

// NewOverrides makes a new Overrides.
func NewOverrides(defaults Limits, tenantLimits TenantLimits) (*Overrides, error) {
	return &Overrides{
		tenantLimits:  tenantLimits,
		defaultLimits: &defaults,
	}, nil
}

// IngestionRateStrategy returns whether the ingestion rate limit should be individually applied
// to each distributor instance (local) or evenly shared across the cluster (global).
func (o *Overrides) IngestionRateStrategy() string {
	// The ingestion rate strategy can't be overridden on a per-tenant basis,
	// so here we just pick the value for a not-existing user ID (empty string).
	return o.getOverridesForUser("").IngestionRateStrategy
}

// IngestionRateBytes returns the limit on ingester rate (MBs per second).
func (o *Overrides) IngestionRateBytes(userID string) float64 {
	return o.getOverridesForUser(userID).IngestionRateMB * bytesInMB
}

// IngestionBurstSizeBytes returns the burst size for ingestion rate.
func (o *Overrides) IngestionBurstSizeBytes(userID string) int {
	return int(o.getOverridesForUser(userID).IngestionBurstSizeMB * bytesInMB)
}

// MaxLabelNameLength returns maximum length a label name can be.
func (o *Overrides) MaxLabelNameLength(userID string) int {
	return o.getOverridesForUser(userID).MaxLabelNameLength
}

// MaxLabelValueLength returns maximum length a label value can be. This also is
// the maximum length of a metric name.
func (o *Overrides) MaxLabelValueLength(userID string) int {
	return o.getOverridesForUser(userID).MaxLabelValueLength
}

// MaxLabelNamesPerSeries returns maximum number of label/value pairs timeseries.
func (o *Overrides) MaxLabelNamesPerSeries(userID string) int {
	return o.getOverridesForUser(userID).MaxLabelNamesPerSeries
}

// RejectOldSamples returns true when we should reject samples older than certain
// age.
func (o *Overrides) RejectOldSamples(userID string) bool {
	return o.getOverridesForUser(userID).RejectOldSamples
}

// RejectOldSamplesMaxAge returns the age at which samples should be rejected.
func (o *Overrides) RejectOldSamplesMaxAge(userID string) time.Duration {
	return o.getOverridesForUser(userID).RejectOldSamplesMaxAge
}

// CreationGracePeriod is misnamed, and actually returns how far into the future
// we should accept samples.
func (o *Overrides) CreationGracePeriod(userID string) time.Duration {
	return o.getOverridesForUser(userID).CreationGracePeriod
}

// MaxLocalStreamsPerUser returns the maximum number of streams a user is allowed to store
// in a single ingester.
func (o *Overrides) MaxLocalStreamsPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxLocalStreamsPerUser
}

// MaxGlobalStreamsPerUser returns the maximum number of streams a user is allowed to store
// across the cluster.
func (o *Overrides) MaxGlobalStreamsPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxGlobalStreamsPerUser
}

// MaxChunksPerQuery returns the maximum number of chunks allowed per query.
func (o *Overrides) MaxChunksPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxChunksPerQuery
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (o *Overrides) MaxQueryLength(userID string) time.Duration {
	return o.getOverridesForUser(userID).MaxQueryLength
}

// MaxQueryLength returns the limit of the series of metric queries.
func (o *Overrides) MaxQuerySeries(userID string) int {
	return o.getOverridesForUser(userID).MaxQuerySeries
}

// MaxQueryParallelism returns the limit to the number of sub-queries the
// frontend will process in parallel.
func (o *Overrides) MaxQueryParallelism(userID string) int {
	return o.getOverridesForUser(userID).MaxQueryParallelism
}

// EnforceMetricName whether to enforce the presence of a metric name.
func (o *Overrides) EnforceMetricName(userID string) bool {
	return o.getOverridesForUser(userID).EnforceMetricName
}

// CardinalityLimit whether to enforce the presence of a metric name.
func (o *Overrides) CardinalityLimit(userID string) int {
	return o.getOverridesForUser(userID).CardinalityLimit
}

// MaxStreamsMatchersPerQuery returns the limit to number of streams matchers per query.
func (o *Overrides) MaxStreamsMatchersPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxStreamsMatchersPerQuery
}

// QuerySplitDuration returns the tenant specific splitby interval applied in the query frontend.
func (o *Overrides) QuerySplitDuration(userID string) time.Duration {
	return o.getOverridesForUser(userID).QuerySplitDuration
}

// MaxConcurrentTailRequests returns the limit to number of concurrent tail requests.
func (o *Overrides) MaxConcurrentTailRequests(userID string) int {
	return o.getOverridesForUser(userID).MaxConcurrentTailRequests
}

// MaxLineSize returns the maximum size in bytes the distributor should allow.
func (o *Overrides) MaxLineSize(userID string) int {
	return o.getOverridesForUser(userID).MaxLineSize.Val()
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (o *Overrides) MaxEntriesLimitPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxEntriesLimitPerQuery
}

func (o *Overrides) MaxCacheFreshness(userID string) time.Duration {
	return o.getOverridesForUser(userID).MaxCacheFreshness
}

// MaxQueryLookback returns the max lookback period of queries.
func (o *Overrides) MaxQueryLookback(userID string) time.Duration {
	return o.getOverridesForUser(userID).MaxQueryLookback
}

func (o *Overrides) getOverridesForUser(userID string) *Limits {
	if o.tenantLimits != nil {
		l := o.tenantLimits(userID)
		if l != nil {
			return l
		}
	}
	return o.defaultLimits
}
