package validation

import (
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/ruler/util"
	"github.com/grafana/loki/pkg/util/flagext"
)

const (
	// LocalRateLimitStrat represents a ingestion rate limiting strategy that enforces the limit
	// on a per distributor basis.
	//
	// The actual effective rate limit will be N times higher, where N is the number of distributor replicas.
	LocalIngestionRateStrategy = "local"

	// GlobalRateLimitStrat represents a ingestion rate limiting strategy that enforces the rate
	// limiting globally, configuring a per-distributor local rate limiter as "ingestion_rate / N",
	// where N is the number of distributor replicas (it's automatically adjusted if the
	// number of replicas change).
	//
	// The global strategy requires the distributors to form their own ring, which
	// is used to keep track of the current number of healthy distributor replicas.
	GlobalIngestionRateStrategy = "global"

	bytesInMB = 1048576

	defaultPerStreamRateLimit  = 3 << 20 // 3MB
	defaultPerStreamBurstLimit = 5 * defaultPerStreamRateLimit
)

// Limits describe all the limits for users; can be used to describe global default
// limits via flags, or per-user limits via yaml config.
// NOTE: we use custom `model.Duration` instead of standard `time.Duration` because,
// to support user-friendly duration format (e.g: "1h30m45s") in JSON value.
type Limits struct {
	// Distributor enforced limits.
	IngestionRateStrategy  string           `yaml:"ingestion_rate_strategy" json:"ingestion_rate_strategy"`
	IngestionRateMB        float64          `yaml:"ingestion_rate_mb" json:"ingestion_rate_mb"`
	IngestionBurstSizeMB   float64          `yaml:"ingestion_burst_size_mb" json:"ingestion_burst_size_mb"`
	MaxLabelNameLength     int              `yaml:"max_label_name_length" json:"max_label_name_length"`
	MaxLabelValueLength    int              `yaml:"max_label_value_length" json:"max_label_value_length"`
	MaxLabelNamesPerSeries int              `yaml:"max_label_names_per_series" json:"max_label_names_per_series"`
	RejectOldSamples       bool             `yaml:"reject_old_samples" json:"reject_old_samples"`
	RejectOldSamplesMaxAge model.Duration   `yaml:"reject_old_samples_max_age" json:"reject_old_samples_max_age"`
	CreationGracePeriod    model.Duration   `yaml:"creation_grace_period" json:"creation_grace_period"`
	EnforceMetricName      bool             `yaml:"enforce_metric_name" json:"enforce_metric_name"`
	MaxLineSize            flagext.ByteSize `yaml:"max_line_size" json:"max_line_size"`
	MaxLineSizeTruncate    bool             `yaml:"max_line_size_truncate" json:"max_line_size_truncate"`

	// Ingester enforced limits.
	MaxLocalStreamsPerUser  int              `yaml:"max_streams_per_user" json:"max_streams_per_user"`
	MaxGlobalStreamsPerUser int              `yaml:"max_global_streams_per_user" json:"max_global_streams_per_user"`
	UnorderedWrites         bool             `yaml:"unordered_writes" json:"unordered_writes"`
	PerStreamRateLimit      flagext.ByteSize `yaml:"per_stream_rate_limit" json:"per_stream_rate_limit"`
	PerStreamRateLimitBurst flagext.ByteSize `yaml:"per_stream_rate_limit_burst" json:"per_stream_rate_limit_burst"`

	// Querier enforced limits.
	MaxChunksPerQuery          int            `yaml:"max_chunks_per_query" json:"max_chunks_per_query"`
	MaxQuerySeries             int            `yaml:"max_query_series" json:"max_query_series"`
	MaxQueryLookback           model.Duration `yaml:"max_query_lookback" json:"max_query_lookback"`
	MaxQueryLength             model.Duration `yaml:"max_query_length" json:"max_query_length"`
	MaxQueryParallelism        int            `yaml:"max_query_parallelism" json:"max_query_parallelism"`
	CardinalityLimit           int            `yaml:"cardinality_limit" json:"cardinality_limit"`
	MaxStreamsMatchersPerQuery int            `yaml:"max_streams_matchers_per_query" json:"max_streams_matchers_per_query"`
	MaxConcurrentTailRequests  int            `yaml:"max_concurrent_tail_requests" json:"max_concurrent_tail_requests"`
	MaxEntriesLimitPerQuery    int            `yaml:"max_entries_limit_per_query" json:"max_entries_limit_per_query"`
	MaxCacheFreshness          model.Duration `yaml:"max_cache_freshness_per_query" json:"max_cache_freshness_per_query"`
	MaxQueriersPerTenant       int            `yaml:"max_queriers_per_tenant" json:"max_queriers_per_tenant"`
	QueryReadyIndexNumDays     int            `yaml:"query_ready_index_num_days" json:"query_ready_index_num_days"`

	// Query frontend enforced limits. The default is actually parameterized by the queryrange config.
	QuerySplitDuration  model.Duration `yaml:"split_queries_by_interval" json:"split_queries_by_interval"`
	MinShardingLookback model.Duration `yaml:"min_sharding_lookback" json:"min_sharding_lookback"`

	// Ruler defaults and limits.
	RulerEvaluationDelay        model.Duration `yaml:"ruler_evaluation_delay_duration" json:"ruler_evaluation_delay_duration"`
	RulerMaxRulesPerRuleGroup   int            `yaml:"ruler_max_rules_per_rule_group" json:"ruler_max_rules_per_rule_group"`
	RulerMaxRuleGroupsPerTenant int            `yaml:"ruler_max_rule_groups_per_tenant" json:"ruler_max_rule_groups_per_tenant"`

	// TODO(dannyk): add HTTP client overrides (basic auth / tls config, etc)
	// Ruler remote-write limits.

	// this field is the inversion of the general remote_write.enabled because the zero value of a boolean is false,
	// and if it were ruler_remote_write_enabled, it would be impossible to know if the value was explicitly set or default
	RulerRemoteWriteDisabled               bool                         `yaml:"ruler_remote_write_disabled" json:"ruler_remote_write_disabled"`
	RulerRemoteWriteURL                    string                       `yaml:"ruler_remote_write_url" json:"ruler_remote_write_url"`
	RulerRemoteWriteTimeout                time.Duration                `yaml:"ruler_remote_write_timeout" json:"ruler_remote_write_timeout"`
	RulerRemoteWriteHeaders                OverwriteMarshalingStringMap `yaml:"ruler_remote_write_headers" json:"ruler_remote_write_headers"`
	RulerRemoteWriteRelabelConfigs         []*util.RelabelConfig        `yaml:"ruler_remote_write_relabel_configs,omitempty" json:"ruler_remote_write_relabel_configs,omitempty"`
	RulerRemoteWriteQueueCapacity          int                          `yaml:"ruler_remote_write_queue_capacity" json:"ruler_remote_write_queue_capacity"`
	RulerRemoteWriteQueueMinShards         int                          `yaml:"ruler_remote_write_queue_min_shards" json:"ruler_remote_write_queue_min_shards"`
	RulerRemoteWriteQueueMaxShards         int                          `yaml:"ruler_remote_write_queue_max_shards" json:"ruler_remote_write_queue_max_shards"`
	RulerRemoteWriteQueueMaxSamplesPerSend int                          `yaml:"ruler_remote_write_queue_max_samples_per_send" json:"ruler_remote_write_queue_max_samples_per_send"`
	RulerRemoteWriteQueueBatchSendDeadline time.Duration                `yaml:"ruler_remote_write_queue_batch_send_deadline" json:"ruler_remote_write_queue_batch_send_deadline"`
	RulerRemoteWriteQueueMinBackoff        time.Duration                `yaml:"ruler_remote_write_queue_min_backoff" json:"ruler_remote_write_queue_min_backoff"`
	RulerRemoteWriteQueueMaxBackoff        time.Duration                `yaml:"ruler_remote_write_queue_max_backoff" json:"ruler_remote_write_queue_max_backoff"`
	RulerRemoteWriteQueueRetryOnRateLimit  bool                         `yaml:"ruler_remote_write_queue_retry_on_ratelimit" json:"ruler_remote_write_queue_retry_on_ratelimit"`

	// Global and per tenant retention
	RetentionPeriod model.Duration    `yaml:"retention_period" json:"retention_period"`
	StreamRetention []StreamRetention `yaml:"retention_stream,omitempty" json:"retention_stream,omitempty"`

	// Config for overrides, convenient if it goes here.
	PerTenantOverrideConfig string         `yaml:"per_tenant_override_config" json:"per_tenant_override_config"`
	PerTenantOverridePeriod model.Duration `yaml:"per_tenant_override_period" json:"per_tenant_override_period"`
}

type StreamRetention struct {
	Period   model.Duration    `yaml:"period" json:"period"`
	Priority int               `yaml:"priority" json:"priority"`
	Selector string            `yaml:"selector" json:"selector"`
	Matchers []*labels.Matcher `yaml:"-" json:"-"` // populated during validation.
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (l *Limits) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&l.IngestionRateStrategy, "distributor.ingestion-rate-limit-strategy", "global", "Whether the ingestion rate limit should be applied individually to each distributor instance (local), or evenly shared across the cluster (global).")
	f.Float64Var(&l.IngestionRateMB, "distributor.ingestion-rate-limit-mb", 4, "Per-user ingestion rate limit in sample size per second. Units in MB.")
	f.Float64Var(&l.IngestionBurstSizeMB, "distributor.ingestion-burst-size-mb", 6, "Per-user allowed ingestion burst size (in sample size). Units in MB.")
	f.Var(&l.MaxLineSize, "distributor.max-line-size", "maximum line length allowed, i.e. 100mb. Default (0) means unlimited.")
	f.BoolVar(&l.MaxLineSizeTruncate, "distributor.max-line-size-truncate", false, "Whether to truncate lines that exceed max_line_size")
	f.IntVar(&l.MaxLabelNameLength, "validation.max-length-label-name", 1024, "Maximum length accepted for label names")
	f.IntVar(&l.MaxLabelValueLength, "validation.max-length-label-value", 2048, "Maximum length accepted for label value. This setting also applies to the metric name")
	f.IntVar(&l.MaxLabelNamesPerSeries, "validation.max-label-names-per-series", 30, "Maximum number of label names per series.")
	f.BoolVar(&l.RejectOldSamples, "validation.reject-old-samples", true, "Reject old samples.")

	_ = l.RejectOldSamplesMaxAge.Set("7d")
	f.Var(&l.RejectOldSamplesMaxAge, "validation.reject-old-samples.max-age", "Maximum accepted sample age before rejecting.")
	_ = l.CreationGracePeriod.Set("10m")
	f.Var(&l.CreationGracePeriod, "validation.create-grace-period", "Duration which table will be created/deleted before/after it's needed; we won't accept sample from before this time.")
	f.BoolVar(&l.EnforceMetricName, "validation.enforce-metric-name", true, "Enforce every sample has a metric name.")
	f.IntVar(&l.MaxEntriesLimitPerQuery, "validation.max-entries-limit", 5000, "Per-user entries limit per query")

	f.IntVar(&l.MaxLocalStreamsPerUser, "ingester.max-streams-per-user", 0, "Maximum number of active streams per user, per ingester. 0 to disable.")
	f.IntVar(&l.MaxGlobalStreamsPerUser, "ingester.max-global-streams-per-user", 5000, "Maximum number of active streams per user, across the cluster. 0 to disable.")
	f.BoolVar(&l.UnorderedWrites, "ingester.unordered-writes", true, "Allow out of order writes.")

	_ = l.PerStreamRateLimit.Set(strconv.Itoa(defaultPerStreamRateLimit))
	f.Var(&l.PerStreamRateLimit, "ingester.per-stream-rate-limit", "Maximum byte rate per second per stream, also expressible in human readable forms (1MB, 256KB, etc).")
	_ = l.PerStreamRateLimitBurst.Set(strconv.Itoa(defaultPerStreamBurstLimit))
	f.Var(&l.PerStreamRateLimitBurst, "ingester.per-stream-rate-limit-burst", "Maximum burst bytes per stream, also expressible in human readable forms (1MB, 256KB, etc).")

	f.IntVar(&l.MaxChunksPerQuery, "store.query-chunk-limit", 2e6, "Maximum number of chunks that can be fetched in a single query.")

	_ = l.MaxQueryLength.Set("721h")
	f.Var(&l.MaxQueryLength, "store.max-query-length", "Limit to length of chunk store queries, 0 to disable.")
	f.IntVar(&l.MaxQuerySeries, "querier.max-query-series", 500, "Limit the maximum of unique series returned by a metric query. When the limit is reached an error is returned.")

	_ = l.MaxQueryLookback.Set("0s")
	f.Var(&l.MaxQueryLookback, "querier.max-query-lookback", "Limit how long back data (series and metadata) can be queried, up until <lookback> duration ago. This limit is enforced in the query-frontend, querier and ruler. If the requested time range is outside the allowed range, the request will not fail but will be manipulated to only query data within the allowed time range. 0 to disable.")
	f.IntVar(&l.MaxQueryParallelism, "querier.max-query-parallelism", 32, "Maximum number of queries will be scheduled in parallel by the frontend.")
	f.IntVar(&l.CardinalityLimit, "store.cardinality-limit", 1e5, "Cardinality limit for index queries.")
	f.IntVar(&l.MaxStreamsMatchersPerQuery, "querier.max-streams-matcher-per-query", 1000, "Limit the number of streams matchers per query")
	f.IntVar(&l.MaxConcurrentTailRequests, "querier.max-concurrent-tail-requests", 10, "Limit the number of concurrent tail requests")

	_ = l.MinShardingLookback.Set("0s")
	f.Var(&l.MinShardingLookback, "frontend.min-sharding-lookback", "Limit the sharding time range.Queries with time range that fall between now and now minus the sharding lookback are not sharded. 0 to disable.")

	_ = l.MaxCacheFreshness.Set("1m")
	f.Var(&l.MaxCacheFreshness, "frontend.max-cache-freshness", "Most recent allowed cacheable result per-tenant, to prevent caching very recent results that might still be in flux.")

	f.IntVar(&l.MaxQueriersPerTenant, "frontend.max-queriers-per-tenant", 0, "Maximum number of queriers that can handle requests for a single tenant. If set to 0 or value higher than number of available queriers, *all* queriers will handle requests for the tenant. Each frontend (or query-scheduler, if used) will select the same set of queriers for the same tenant (given that all queriers are connected to all frontends / query-schedulers). This option only works with queriers connecting to the query-frontend / query-scheduler, not when using downstream URL.")
	f.IntVar(&l.QueryReadyIndexNumDays, "store.query-ready-index-num-days", 0, "Number of days of index to be kept always downloaded for queries. Applies only to per user index in boltdb-shipper index store. 0 to disable.")

	_ = l.RulerEvaluationDelay.Set("0s")
	f.Var(&l.RulerEvaluationDelay, "ruler.evaluation-delay-duration", "Duration to delay the evaluation of rules to ensure the underlying metrics have been pushed to Cortex.")

	f.IntVar(&l.RulerMaxRulesPerRuleGroup, "ruler.max-rules-per-rule-group", 0, "Maximum number of rules per rule group per-tenant. 0 to disable.")
	f.IntVar(&l.RulerMaxRuleGroupsPerTenant, "ruler.max-rule-groups-per-tenant", 0, "Maximum number of rule groups per-tenant. 0 to disable.")

	f.StringVar(&l.PerTenantOverrideConfig, "limits.per-user-override-config", "", "File name of per-user overrides.")
	_ = l.RetentionPeriod.Set("744h")
	f.Var(&l.RetentionPeriod, "store.retention", "How long before chunks will be deleted from the store. (requires compactor retention enabled).")

	_ = l.PerTenantOverridePeriod.Set("10s")
	f.Var(&l.PerTenantOverridePeriod, "limits.per-user-override-period", "Period with this to reload the overrides.")

	_ = l.QuerySplitDuration.Set("30m")
	f.Var(&l.QuerySplitDuration, "querier.split-queries-by-interval", "Split queries by an interval and execute in parallel, 0 disables it. This also determines how cache keys are chosen when result caching is enabled")
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (l *Limits) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.  See prometheus/config.
	type plain Limits

	// During startup we wont have a default value so we don't want to overwrite them
	if defaultLimits != nil {
		b, err := yaml.Marshal(defaultLimits)
		if err != nil {
			return errors.Wrap(err, "cloning limits (marshaling)")
		}
		if err := yaml.Unmarshal(b, (*plain)(l)); err != nil {
			return errors.Wrap(err, "cloning limits (unmarshaling)")
		}
	}
	return unmarshal((*plain)(l))
}

// Validate validates that this limits config is valid.
func (l *Limits) Validate() error {

	if l.StreamRetention != nil {
		for i, rule := range l.StreamRetention {
			matchers, err := logql.ParseMatchers(rule.Selector)
			if err != nil {
				return fmt.Errorf("invalid labels matchers: %w", err)
			}
			if time.Duration(rule.Period) < 24*time.Hour {
				return fmt.Errorf("retention period must be >= 24h was %s", rule.Period)
			}
			// populate matchers during validation
			l.StreamRetention[i].Matchers = matchers
		}
	}
	return nil
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

type TenantLimits interface {
	// TenantLimits is a function that returns limits for given tenant, or
	// nil, if there are no tenant-specific limits.
	TenantLimits(userID string) *Limits
	// AllByUserID gets a mapping of all tenant IDs and limits for that user
	AllByUserID() map[string]*Limits
}

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

func (o *Overrides) AllByUserID() map[string]*Limits {
	if o.tenantLimits != nil {
		return o.tenantLimits.AllByUserID()
	}
	return nil
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
	return time.Duration(o.getOverridesForUser(userID).RejectOldSamplesMaxAge)
}

// CreationGracePeriod is misnamed, and actually returns how far into the future
// we should accept samples.
func (o *Overrides) CreationGracePeriod(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).CreationGracePeriod)
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
	return time.Duration(o.getOverridesForUser(userID).MaxQueryLength)
}

// Compatibility with Cortex interface, this method is set to be removed in 1.12,
// so nooping in Loki until then.
func (o *Overrides) MaxChunksPerQueryFromStore(userID string) int { return 0 }

// MaxQueryLength returns the limit of the series of metric queries.
func (o *Overrides) MaxQuerySeries(userID string) int {
	return o.getOverridesForUser(userID).MaxQuerySeries
}

// MaxQueriersPerUser returns the maximum number of queriers that can handle requests for this user.
func (o *Overrides) MaxQueriersPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxQueriersPerTenant
}

// QueryReadyIndexNumDays returns the number of days for which we have to be query ready for a user.
func (o *Overrides) QueryReadyIndexNumDays(userID string) int {
	return o.getOverridesForUser(userID).QueryReadyIndexNumDays
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

// MinShardingLookback returns the tenant specific min sharding lookback (e.g from when we should start sharding).
func (o *Overrides) MinShardingLookback(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MinShardingLookback)
}

// QuerySplitDuration returns the tenant specific splitby interval applied in the query frontend.
func (o *Overrides) QuerySplitDuration(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).QuerySplitDuration)
}

// MaxConcurrentTailRequests returns the limit to number of concurrent tail requests.
func (o *Overrides) MaxConcurrentTailRequests(userID string) int {
	return o.getOverridesForUser(userID).MaxConcurrentTailRequests
}

// MaxLineSize returns the maximum size in bytes the distributor should allow.
func (o *Overrides) MaxLineSize(userID string) int {
	return o.getOverridesForUser(userID).MaxLineSize.Val()
}

// MaxLineSizeShouldTruncate returns whether lines longer than max should be truncated.
func (o *Overrides) MaxLineSizeTruncate(userID string) bool {
	return o.getOverridesForUser(userID).MaxLineSizeTruncate
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (o *Overrides) MaxEntriesLimitPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxEntriesLimitPerQuery
}

func (o *Overrides) MaxCacheFreshness(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxCacheFreshness)
}

// MaxQueryLookback returns the max lookback period of queries.
func (o *Overrides) MaxQueryLookback(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxQueryLookback)
}

// EvaluationDelay returns the rules evaluation delay for a given user.
func (o *Overrides) EvaluationDelay(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).RulerEvaluationDelay)
}

// RulerTenantShardSize returns shard size (number of rulers) used by this tenant when using shuffle-sharding strategy.
// Not used in Loki.
func (o *Overrides) RulerTenantShardSize(userID string) int {
	return 0
}

// RulerMaxRulesPerRuleGroup returns the maximum number of rules per rule group for a given user.
func (o *Overrides) RulerMaxRulesPerRuleGroup(userID string) int {
	return o.getOverridesForUser(userID).RulerMaxRulesPerRuleGroup
}

// RulerMaxRuleGroupsPerTenant returns the maximum number of rule groups for a given user.
func (o *Overrides) RulerMaxRuleGroupsPerTenant(userID string) int {
	return o.getOverridesForUser(userID).RulerMaxRuleGroupsPerTenant
}

// RulerRemoteWriteDisabled returns whether remote-write is disabled for a given user or not.
func (o *Overrides) RulerRemoteWriteDisabled(userID string) bool {
	return o.getOverridesForUser(userID).RulerRemoteWriteDisabled
}

// RulerRemoteWriteURL returns the remote-write URL to use for a given user.
func (o *Overrides) RulerRemoteWriteURL(userID string) string {
	return o.getOverridesForUser(userID).RulerRemoteWriteURL
}

// RulerRemoteWriteTimeout returns the duration after which to timeout a remote-write request for a given user.
func (o *Overrides) RulerRemoteWriteTimeout(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteTimeout
}

// RulerRemoteWriteHeaders returns the headers to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteHeaders(userID string) map[string]string {
	return o.getOverridesForUser(userID).RulerRemoteWriteHeaders.Map()
}

// RulerRemoteWriteRelabelConfigs returns the write relabel configs to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteRelabelConfigs(userID string) []*util.RelabelConfig {
	return o.getOverridesForUser(userID).RulerRemoteWriteRelabelConfigs
}

// RulerRemoteWriteQueueCapacity returns the queue capacity to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueCapacity(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueCapacity
}

// RulerRemoteWriteQueueMinShards returns the minimum shards to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueMinShards(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMinShards
}

// RulerRemoteWriteQueueMaxShards returns the maximum shards to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueMaxShards(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMaxShards
}

// RulerRemoteWriteQueueMaxSamplesPerSend returns the max samples to send in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueMaxSamplesPerSend(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMaxSamplesPerSend
}

// RulerRemoteWriteQueueBatchSendDeadline returns the maximum time a sample will be buffered before being discarded for a given user.
func (o *Overrides) RulerRemoteWriteQueueBatchSendDeadline(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueBatchSendDeadline
}

// RulerRemoteWriteQueueMinBackoff returns the minimum time for an exponential backoff for a given user.
func (o *Overrides) RulerRemoteWriteQueueMinBackoff(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMinBackoff
}

// RulerRemoteWriteQueueMaxBackoff returns the maximum time for an exponential backoff for a given user.
func (o *Overrides) RulerRemoteWriteQueueMaxBackoff(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMaxBackoff
}

// RulerRemoteWriteQueueRetryOnRateLimit returns whether to retry failed remote-write requests (429 response) for a given user.
func (o *Overrides) RulerRemoteWriteQueueRetryOnRateLimit(userID string) bool {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueRetryOnRateLimit
}

// RetentionPeriod returns the retention period for a given user.
func (o *Overrides) RetentionPeriod(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).RetentionPeriod)
}

// StreamRetention returns the retention period for a given user.
func (o *Overrides) StreamRetention(userID string) []StreamRetention {
	return o.getOverridesForUser(userID).StreamRetention
}

func (o *Overrides) UnorderedWrites(userID string) bool {
	return o.getOverridesForUser(userID).UnorderedWrites
}

func (o *Overrides) DefaultLimits() *Limits {
	return o.defaultLimits
}

func (o *Overrides) PerStreamRateLimit(userID string) RateLimit {
	user := o.getOverridesForUser(userID)

	return RateLimit{
		Limit: rate.Limit(float64(user.PerStreamRateLimit.Val())),
		Burst: user.PerStreamRateLimitBurst.Val(),
	}
}

func (o *Overrides) getOverridesForUser(userID string) *Limits {
	if o.tenantLimits != nil {
		l := o.tenantLimits.TenantLimits(userID)
		if l != nil {
			return l
		}
	}
	return o.defaultLimits
}

// OverwriteMarshalingStringMap will overwrite the src map when unmarshaling
// as opposed to merging.
type OverwriteMarshalingStringMap struct {
	m map[string]string
}

func NewOverwriteMarshalingStringMap(m map[string]string) OverwriteMarshalingStringMap {
	return OverwriteMarshalingStringMap{m: m}
}

func (sm *OverwriteMarshalingStringMap) Map() map[string]string {
	return sm.m
}

// MarshalJSON explicitly uses the the type receiver and not pointer receiver
// or it won't be called
func (sm OverwriteMarshalingStringMap) MarshalJSON() ([]byte, error) {
	return json.Marshal(sm.m)
}

func (sm *OverwriteMarshalingStringMap) UnmarshalJSON(val []byte) error {
	var def map[string]string
	if err := json.Unmarshal(val, &def); err != nil {
		return err
	}
	sm.m = def

	return nil

}

// MarshalYAML explicitly uses the the type receiver and not pointer receiver
// or it won't be called
func (sm OverwriteMarshalingStringMap) MarshalYAML() (interface{}, error) {
	return sm.m, nil
}

func (sm *OverwriteMarshalingStringMap) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var def map[string]string

	err := unmarshal(&def)
	if err != nil {
		return err
	}
	sm.m = def

	return nil
}
