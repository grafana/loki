package validation

import (
	"errors"
	"flag"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/cortexproject/cortex/pkg/util/flagext"
)

var (
	errMaxGlobalSeriesPerUserValidation = errors.New("The ingester.max-global-series-per-user limit is unsupported if distributor.shard-by-all-labels is disabled")
)

// Supported values for enum limits
const (
	LocalIngestionRateStrategy  = "local"
	GlobalIngestionRateStrategy = "global"
)

//LimitError are errors that do not comply with the limits specified.
type LimitError string

func (e LimitError) Error() string {
	return string(e)
}

// Limits describe all the limits for users; can be used to describe global default
// limits via flags, or per-user limits via yaml config.
type Limits struct {
	// Distributor enforced limits.
	IngestionRate             float64             `yaml:"ingestion_rate"`
	IngestionRateStrategy     string              `yaml:"ingestion_rate_strategy"`
	IngestionBurstSize        int                 `yaml:"ingestion_burst_size"`
	AcceptHASamples           bool                `yaml:"accept_ha_samples"`
	HAClusterLabel            string              `yaml:"ha_cluster_label"`
	HAReplicaLabel            string              `yaml:"ha_replica_label"`
	HAMaxClusters             int                 `yaml:"ha_max_clusters"`
	DropLabels                flagext.StringSlice `yaml:"drop_labels"`
	MaxLabelNameLength        int                 `yaml:"max_label_name_length"`
	MaxLabelValueLength       int                 `yaml:"max_label_value_length"`
	MaxLabelNamesPerSeries    int                 `yaml:"max_label_names_per_series"`
	MaxMetadataLength         int                 `yaml:"max_metadata_length"`
	RejectOldSamples          bool                `yaml:"reject_old_samples"`
	RejectOldSamplesMaxAge    time.Duration       `yaml:"reject_old_samples_max_age"`
	CreationGracePeriod       time.Duration       `yaml:"creation_grace_period"`
	EnforceMetadataMetricName bool                `yaml:"enforce_metadata_metric_name"`
	EnforceMetricName         bool                `yaml:"enforce_metric_name"`
	IngestionTenantShardSize  int                 `yaml:"ingestion_tenant_shard_size"`
	MetricRelabelConfigs      []*relabel.Config   `yaml:"metric_relabel_configs,omitempty" doc:"nocli|description=List of metric relabel configurations. Note that in most situations, it is more effective to use metrics relabeling directly in the Prometheus server, e.g. remote_write.write_relabel_configs."`

	// Ingester enforced limits.
	// Series
	MaxSeriesPerQuery        int `yaml:"max_series_per_query"`
	MaxSamplesPerQuery       int `yaml:"max_samples_per_query"`
	MaxLocalSeriesPerUser    int `yaml:"max_series_per_user"`
	MaxLocalSeriesPerMetric  int `yaml:"max_series_per_metric"`
	MaxGlobalSeriesPerUser   int `yaml:"max_global_series_per_user"`
	MaxGlobalSeriesPerMetric int `yaml:"max_global_series_per_metric"`
	MinChunkLength           int `yaml:"min_chunk_length"`
	// Metadata
	MaxLocalMetricsWithMetadataPerUser  int `yaml:"max_metadata_per_user"`
	MaxLocalMetadataPerMetric           int `yaml:"max_metadata_per_metric"`
	MaxGlobalMetricsWithMetadataPerUser int `yaml:"max_global_metadata_per_user"`
	MaxGlobalMetadataPerMetric          int `yaml:"max_global_metadata_per_metric"`

	// Querier enforced limits.
	MaxChunksPerQuery    int            `yaml:"max_chunks_per_query"`
	MaxQueryLookback     model.Duration `yaml:"max_query_lookback"`
	MaxQueryLength       time.Duration  `yaml:"max_query_length"`
	MaxQueryParallelism  int            `yaml:"max_query_parallelism"`
	CardinalityLimit     int            `yaml:"cardinality_limit"`
	MaxCacheFreshness    time.Duration  `yaml:"max_cache_freshness"`
	MaxQueriersPerTenant int            `yaml:"max_queriers_per_tenant"`

	// Ruler defaults and limits.
	RulerEvaluationDelay        time.Duration `yaml:"ruler_evaluation_delay_duration"`
	RulerTenantShardSize        int           `yaml:"ruler_tenant_shard_size"`
	RulerMaxRulesPerRuleGroup   int           `yaml:"ruler_max_rules_per_rule_group"`
	RulerMaxRuleGroupsPerTenant int           `yaml:"ruler_max_rule_groups_per_tenant"`

	// Store-gateway.
	StoreGatewayTenantShardSize int `yaml:"store_gateway_tenant_shard_size"`

	// Config for overrides, convenient if it goes here. [Deprecated in favor of RuntimeConfig flag in cortex.Config]
	PerTenantOverrideConfig string        `yaml:"per_tenant_override_config"`
	PerTenantOverridePeriod time.Duration `yaml:"per_tenant_override_period"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (l *Limits) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&l.IngestionTenantShardSize, "distributor.ingestion-tenant-shard-size", 0, "The default tenant's shard size when the shuffle-sharding strategy is used. Must be set both on ingesters and distributors. When this setting is specified in the per-tenant overrides, a value of 0 disables shuffle sharding for the tenant.")
	f.Float64Var(&l.IngestionRate, "distributor.ingestion-rate-limit", 25000, "Per-user ingestion rate limit in samples per second.")
	f.StringVar(&l.IngestionRateStrategy, "distributor.ingestion-rate-limit-strategy", "local", "Whether the ingestion rate limit should be applied individually to each distributor instance (local), or evenly shared across the cluster (global).")
	f.IntVar(&l.IngestionBurstSize, "distributor.ingestion-burst-size", 50000, "Per-user allowed ingestion burst size (in number of samples).")
	f.BoolVar(&l.AcceptHASamples, "distributor.ha-tracker.enable-for-all-users", false, "Flag to enable, for all users, handling of samples with external labels identifying replicas in an HA Prometheus setup.")
	f.StringVar(&l.HAClusterLabel, "distributor.ha-tracker.cluster", "cluster", "Prometheus label to look for in samples to identify a Prometheus HA cluster.")
	f.StringVar(&l.HAReplicaLabel, "distributor.ha-tracker.replica", "__replica__", "Prometheus label to look for in samples to identify a Prometheus HA replica.")
	f.IntVar(&l.HAMaxClusters, "distributor.ha-tracker.max-clusters", 0, "Maximum number of clusters that HA tracker will keep track of for single user. 0 to disable the limit.")
	f.Var(&l.DropLabels, "distributor.drop-label", "This flag can be used to specify label names that to drop during sample ingestion within the distributor and can be repeated in order to drop multiple labels.")
	f.IntVar(&l.MaxLabelNameLength, "validation.max-length-label-name", 1024, "Maximum length accepted for label names")
	f.IntVar(&l.MaxLabelValueLength, "validation.max-length-label-value", 2048, "Maximum length accepted for label value. This setting also applies to the metric name")
	f.IntVar(&l.MaxLabelNamesPerSeries, "validation.max-label-names-per-series", 30, "Maximum number of label names per series.")
	f.IntVar(&l.MaxMetadataLength, "validation.max-metadata-length", 1024, "Maximum length accepted for metric metadata. Metadata refers to Metric Name, HELP and UNIT.")
	f.BoolVar(&l.RejectOldSamples, "validation.reject-old-samples", false, "Reject old samples.")
	f.DurationVar(&l.RejectOldSamplesMaxAge, "validation.reject-old-samples.max-age", 14*24*time.Hour, "Maximum accepted sample age before rejecting.")
	f.DurationVar(&l.CreationGracePeriod, "validation.create-grace-period", 10*time.Minute, "Duration which table will be created/deleted before/after it's needed; we won't accept sample from before this time.")
	f.BoolVar(&l.EnforceMetricName, "validation.enforce-metric-name", true, "Enforce every sample has a metric name.")
	f.BoolVar(&l.EnforceMetadataMetricName, "validation.enforce-metadata-metric-name", true, "Enforce every metadata has a metric name.")

	f.IntVar(&l.MaxSeriesPerQuery, "ingester.max-series-per-query", 100000, "The maximum number of series for which a query can fetch samples from each ingester. This limit is enforced only in the ingesters (when querying samples not flushed to the storage yet) and it's a per-instance limit. This limit is ignored when running the Cortex blocks storage.")
	f.IntVar(&l.MaxSamplesPerQuery, "ingester.max-samples-per-query", 1000000, "The maximum number of samples that a query can return. This limit only applies when running the Cortex chunks storage with -querier.ingester-streaming=false.")
	f.IntVar(&l.MaxLocalSeriesPerUser, "ingester.max-series-per-user", 5000000, "The maximum number of active series per user, per ingester. 0 to disable.")
	f.IntVar(&l.MaxLocalSeriesPerMetric, "ingester.max-series-per-metric", 50000, "The maximum number of active series per metric name, per ingester. 0 to disable.")
	f.IntVar(&l.MaxGlobalSeriesPerUser, "ingester.max-global-series-per-user", 0, "The maximum number of active series per user, across the cluster. 0 to disable. Supported only if -distributor.shard-by-all-labels is true.")
	f.IntVar(&l.MaxGlobalSeriesPerMetric, "ingester.max-global-series-per-metric", 0, "The maximum number of active series per metric name, across the cluster. 0 to disable.")
	f.IntVar(&l.MinChunkLength, "ingester.min-chunk-length", 0, "Minimum number of samples in an idle chunk to flush it to the store. Use with care, if chunks are less than this size they will be discarded. This option is ignored when running the Cortex blocks storage. 0 to disable.")

	f.IntVar(&l.MaxLocalMetricsWithMetadataPerUser, "ingester.max-metadata-per-user", 8000, "The maximum number of active metrics with metadata per user, per ingester. 0 to disable.")
	f.IntVar(&l.MaxLocalMetadataPerMetric, "ingester.max-metadata-per-metric", 10, "The maximum number of metadata per metric, per ingester. 0 to disable.")
	f.IntVar(&l.MaxGlobalMetricsWithMetadataPerUser, "ingester.max-global-metadata-per-user", 0, "The maximum number of active metrics with metadata per user, across the cluster. 0 to disable. Supported only if -distributor.shard-by-all-labels is true.")
	f.IntVar(&l.MaxGlobalMetadataPerMetric, "ingester.max-global-metadata-per-metric", 0, "The maximum number of metadata per metric, across the cluster. 0 to disable.")

	f.IntVar(&l.MaxChunksPerQuery, "store.query-chunk-limit", 2e6, "Maximum number of chunks that can be fetched in a single query. This limit is enforced when fetching chunks from the long-term storage. When running the Cortex chunks storage, this limit is enforced in the querier, while when running the Cortex blocks storage this limit is both enforced in the querier and store-gateway. 0 to disable.")
	f.DurationVar(&l.MaxQueryLength, "store.max-query-length", 0, "Limit the query time range (end - start time). This limit is enforced in the query-frontend (on the received query), in the querier (on the query possibly split by the query-frontend) and in the chunks storage. 0 to disable.")
	f.Var(&l.MaxQueryLookback, "querier.max-query-lookback", "Limit how long back data (series and metadata) can be queried, up until <lookback> duration ago. This limit is enforced in the query-frontend, querier and ruler. If the requested time range is outside the allowed range, the request will not fail but will be manipulated to only query data within the allowed time range. 0 to disable.")
	f.IntVar(&l.MaxQueryParallelism, "querier.max-query-parallelism", 14, "Maximum number of split queries will be scheduled in parallel by the frontend.")
	f.IntVar(&l.CardinalityLimit, "store.cardinality-limit", 1e5, "Cardinality limit for index queries. This limit is ignored when running the Cortex blocks storage. 0 to disable.")
	f.DurationVar(&l.MaxCacheFreshness, "frontend.max-cache-freshness", 1*time.Minute, "Most recent allowed cacheable result per-tenant, to prevent caching very recent results that might still be in flux.")
	f.IntVar(&l.MaxQueriersPerTenant, "frontend.max-queriers-per-tenant", 0, "Maximum number of queriers that can handle requests for a single tenant. If set to 0 or value higher than number of available queriers, *all* queriers will handle requests for the tenant. Each frontend (or query-scheduler, if used) will select the same set of queriers for the same tenant (given that all queriers are connected to all frontends / query-schedulers). This option only works with queriers connecting to the query-frontend / query-scheduler, not when using downstream URL.")

	f.DurationVar(&l.RulerEvaluationDelay, "ruler.evaluation-delay-duration", 0, "Duration to delay the evaluation of rules to ensure the underlying metrics have been pushed to Cortex.")
	f.IntVar(&l.RulerTenantShardSize, "ruler.tenant-shard-size", 0, "The default tenant's shard size when the shuffle-sharding strategy is used by ruler. When this setting is specified in the per-tenant overrides, a value of 0 disables shuffle sharding for the tenant.")
	f.IntVar(&l.RulerMaxRulesPerRuleGroup, "ruler.max-rules-per-rule-group", 0, "Maximum number of rules per rule group per-tenant. 0 to disable.")
	f.IntVar(&l.RulerMaxRuleGroupsPerTenant, "ruler.max-rule-groups-per-tenant", 0, "Maximum number of rule groups per-tenant. 0 to disable.")

	f.StringVar(&l.PerTenantOverrideConfig, "limits.per-user-override-config", "", "File name of per-user overrides. [deprecated, use -runtime-config.file instead]")
	f.DurationVar(&l.PerTenantOverridePeriod, "limits.per-user-override-period", 10*time.Second, "Period with which to reload the overrides. [deprecated, use -runtime-config.reload-period instead]")

	// Store-gateway.
	f.IntVar(&l.StoreGatewayTenantShardSize, "store-gateway.tenant-shard-size", 0, "The default tenant's shard size when the shuffle-sharding strategy is used. Must be set when the store-gateway sharding is enabled with the shuffle-sharding strategy. When this setting is specified in the per-tenant overrides, a value of 0 disables shuffle sharding for the tenant.")
}

// Validate the limits config and returns an error if the validation
// doesn't pass
func (l *Limits) Validate(shardByAllLabels bool) error {
	// The ingester.max-global-series-per-user metric is not supported
	// if shard-by-all-labels is disabled
	if l.MaxGlobalSeriesPerUser > 0 && !shardByAllLabels {
		return errMaxGlobalSeriesPerUserValidation
	}

	return nil
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

// IngestionRate returns the limit on ingester rate (samples per second).
func (o *Overrides) IngestionRate(userID string) float64 {
	return o.getOverridesForUser(userID).IngestionRate
}

// IngestionRateStrategy returns whether the ingestion rate limit should be individually applied
// to each distributor instance (local) or evenly shared across the cluster (global).
func (o *Overrides) IngestionRateStrategy() string {
	// The ingestion rate strategy can't be overridden on a per-tenant basis
	return o.defaultLimits.IngestionRateStrategy
}

// IngestionBurstSize returns the burst size for ingestion rate.
func (o *Overrides) IngestionBurstSize(userID string) int {
	return o.getOverridesForUser(userID).IngestionBurstSize
}

// AcceptHASamples returns whether the distributor should track and accept samples from HA replicas for this user.
func (o *Overrides) AcceptHASamples(userID string) bool {
	return o.getOverridesForUser(userID).AcceptHASamples
}

// HAClusterLabel returns the cluster label to look for when deciding whether to accept a sample from a Prometheus HA replica.
func (o *Overrides) HAClusterLabel(userID string) string {
	return o.getOverridesForUser(userID).HAClusterLabel
}

// HAReplicaLabel returns the replica label to look for when deciding whether to accept a sample from a Prometheus HA replica.
func (o *Overrides) HAReplicaLabel(userID string) string {
	return o.getOverridesForUser(userID).HAReplicaLabel
}

// DropLabels returns the list of labels to be dropped when ingesting HA samples for the user.
func (o *Overrides) DropLabels(userID string) flagext.StringSlice {
	return o.getOverridesForUser(userID).DropLabels
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

// MaxMetadataLength returns maximum length metadata can be. Metadata refers
// to the Metric Name, HELP and UNIT.
func (o *Overrides) MaxMetadataLength(userID string) int {
	return o.getOverridesForUser(userID).MaxMetadataLength
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

// MaxSeriesPerQuery returns the maximum number of series a query is allowed to hit.
func (o *Overrides) MaxSeriesPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxSeriesPerQuery
}

// MaxSamplesPerQuery returns the maximum number of samples in a query (from the ingester).
func (o *Overrides) MaxSamplesPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxSamplesPerQuery
}

// MaxLocalSeriesPerUser returns the maximum number of series a user is allowed to store in a single ingester.
func (o *Overrides) MaxLocalSeriesPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxLocalSeriesPerUser
}

// MaxLocalSeriesPerMetric returns the maximum number of series allowed per metric in a single ingester.
func (o *Overrides) MaxLocalSeriesPerMetric(userID string) int {
	return o.getOverridesForUser(userID).MaxLocalSeriesPerMetric
}

// MaxGlobalSeriesPerUser returns the maximum number of series a user is allowed to store across the cluster.
func (o *Overrides) MaxGlobalSeriesPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxGlobalSeriesPerUser
}

// MaxGlobalSeriesPerMetric returns the maximum number of series allowed per metric across the cluster.
func (o *Overrides) MaxGlobalSeriesPerMetric(userID string) int {
	return o.getOverridesForUser(userID).MaxGlobalSeriesPerMetric
}

// MaxChunksPerQuery returns the maximum number of chunks allowed per query.
func (o *Overrides) MaxChunksPerQuery(userID string) int {
	return o.getOverridesForUser(userID).MaxChunksPerQuery
}

// MaxQueryLookback returns the max lookback period of queries.
func (o *Overrides) MaxQueryLookback(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxQueryLookback)
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (o *Overrides) MaxQueryLength(userID string) time.Duration {
	return o.getOverridesForUser(userID).MaxQueryLength
}

// MaxCacheFreshness returns the period after which results are cacheable,
// to prevent caching of very recent results.
func (o *Overrides) MaxCacheFreshness(userID string) time.Duration {
	return o.getOverridesForUser(userID).MaxCacheFreshness
}

// MaxQueriersPerUser returns the maximum number of queriers that can handle requests for this user.
func (o *Overrides) MaxQueriersPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxQueriersPerTenant
}

// MaxQueryParallelism returns the limit to the number of split queries the
// frontend will process in parallel.
func (o *Overrides) MaxQueryParallelism(userID string) int {
	return o.getOverridesForUser(userID).MaxQueryParallelism
}

// EnforceMetricName whether to enforce the presence of a metric name.
func (o *Overrides) EnforceMetricName(userID string) bool {
	return o.getOverridesForUser(userID).EnforceMetricName
}

// EnforceMetadataMetricName whether to enforce the presence of a metric name on metadata.
func (o *Overrides) EnforceMetadataMetricName(userID string) bool {
	return o.getOverridesForUser(userID).EnforceMetadataMetricName
}

// CardinalityLimit returns the maximum number of timeseries allowed in a query.
func (o *Overrides) CardinalityLimit(userID string) int {
	return o.getOverridesForUser(userID).CardinalityLimit
}

// MinChunkLength returns the minimum size of chunk that will be saved by ingesters
func (o *Overrides) MinChunkLength(userID string) int {
	return o.getOverridesForUser(userID).MinChunkLength
}

// MaxLocalMetricsWithMetadataPerUser returns the maximum number of metrics with metadata a user is allowed to store in a single ingester.
func (o *Overrides) MaxLocalMetricsWithMetadataPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxLocalMetricsWithMetadataPerUser
}

// MaxLocalMetadataPerMetric returns the maximum number of metadata allowed per metric in a single ingester.
func (o *Overrides) MaxLocalMetadataPerMetric(userID string) int {
	return o.getOverridesForUser(userID).MaxLocalMetadataPerMetric
}

// MaxGlobalMetricsWithMetadataPerUser returns the maximum number of metrics with metadata a user is allowed to store across the cluster.
func (o *Overrides) MaxGlobalMetricsWithMetadataPerUser(userID string) int {
	return o.getOverridesForUser(userID).MaxGlobalMetricsWithMetadataPerUser
}

// MaxGlobalMetadataPerMetric returns the maximum number of metadata allowed per metric across the cluster.
func (o *Overrides) MaxGlobalMetadataPerMetric(userID string) int {
	return o.getOverridesForUser(userID).MaxGlobalMetadataPerMetric
}

// IngestionTenantShardSize returns the ingesters shard size for a given user.
func (o *Overrides) IngestionTenantShardSize(userID string) int {
	return o.getOverridesForUser(userID).IngestionTenantShardSize
}

// EvaluationDelay returns the rules evaluation delay for a given user.
func (o *Overrides) EvaluationDelay(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerEvaluationDelay
}

// MetricRelabelConfigs returns the metric relabel configs for a given user.
func (o *Overrides) MetricRelabelConfigs(userID string) []*relabel.Config {
	return o.getOverridesForUser(userID).MetricRelabelConfigs
}

// RulerTenantShardSize returns shard size (number of rulers) used by this tenant when using shuffle-sharding strategy.
func (o *Overrides) RulerTenantShardSize(userID string) int {
	return o.getOverridesForUser(userID).RulerTenantShardSize
}

// RulerMaxRulesPerRuleGroup returns the maximum number of rules per rule group for a given user.
func (o *Overrides) RulerMaxRulesPerRuleGroup(userID string) int {
	return o.getOverridesForUser(userID).RulerMaxRulesPerRuleGroup
}

// RulerMaxRuleGroupsPerTenant returns the maximum number of rule groups for a given user.
func (o *Overrides) RulerMaxRuleGroupsPerTenant(userID string) int {
	return o.getOverridesForUser(userID).RulerMaxRuleGroupsPerTenant
}

// StoreGatewayTenantShardSize returns the store-gateway shard size for a given user.
func (o *Overrides) StoreGatewayTenantShardSize(userID string) int {
	return o.getOverridesForUser(userID).StoreGatewayTenantShardSize
}

// MaxHAClusters returns maximum number of clusters that HA tracker will track for a user.
func (o *Overrides) MaxHAClusters(user string) int {
	return o.getOverridesForUser(user).HAMaxClusters
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

// SmallestPositiveIntPerTenant is returning the minimal positive value of the
// supplied limit function for all given tenants.
func SmallestPositiveIntPerTenant(tenantIDs []string, f func(string) int) int {
	var result *int
	for _, tenantID := range tenantIDs {
		v := f(tenantID)
		if result == nil || v < *result {
			result = &v
		}
	}
	if result == nil {
		return 0
	}
	return *result
}

// SmallestPositiveNonZeroIntPerTenant is returning the minimal positive and
// non-zero value of the supplied limit function for all given tenants. In many
// limits a value of 0 means unlimted so the method will return 0 only if all
// inputs have a limit of 0 or an empty tenant list is given.
func SmallestPositiveNonZeroIntPerTenant(tenantIDs []string, f func(string) int) int {
	var result *int
	for _, tenantID := range tenantIDs {
		v := f(tenantID)
		if v > 0 && (result == nil || v < *result) {
			result = &v
		}
	}
	if result == nil {
		return 0
	}
	return *result
}

// SmallestPositiveNonZeroDurationPerTenant is returning the minimal positive
// and non-zero value of the supplied limit function for all given tenants. In
// many limits a value of 0 means unlimted so the method will return 0 only if
// all inputs have a limit of 0 or an empty tenant list is given.
func SmallestPositiveNonZeroDurationPerTenant(tenantIDs []string, f func(string) time.Duration) time.Duration {
	var result *time.Duration
	for _, tenantID := range tenantIDs {
		v := f(tenantID)
		if v > 0 && (result == nil || v < *result) {
			result = &v
		}
	}
	if result == nil {
		return 0
	}
	return *result
}

// MaxDurationPerTenant is returning the maximum duration per tenant. Without
// tenants given it will return a time.Duration(0).
func MaxDurationPerTenant(tenantIDs []string, f func(string) time.Duration) time.Duration {
	result := time.Duration(0)
	for _, tenantID := range tenantIDs {
		v := f(tenantID)
		if v > result {
			result = v
		}
	}
	return result
}
