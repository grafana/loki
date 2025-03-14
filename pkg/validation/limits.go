package validation

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/go-kit/log/level"
	dskit_flagext "github.com/grafana/dskit/flagext"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/sigv4"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	ruler_config "github.com/grafana/loki/v3/pkg/ruler/config"
	"github.com/grafana/loki/v3/pkg/ruler/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/validation"
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

	defaultPerStreamRateLimit   = 3 << 20 // 3MB
	DefaultTSDBMaxBytesPerShard = sharding.DefaultTSDBMaxBytesPerShard
	defaultPerStreamBurstLimit  = 5 * defaultPerStreamRateLimit

	DefaultPerTenantQueryTimeout = "1m"

	defaultMaxStructuredMetadataSize  = "64kb"
	defaultMaxStructuredMetadataCount = 128
	defaultBloomBuildMaxBlockSize     = "200MB"
	defaultBloomBuildMaxBloomSize     = "128MB"
	defaultBloomTaskTargetChunkSize   = "20GB"

	defaultBlockedIngestionStatusCode = 260 // 260 is a custom status code to indicate blocked ingestion
)

// Limits describe all the limits for users; can be used to describe global default
// limits via flags, or per-user limits via yaml config.
// NOTE: we use custom `model.Duration` instead of standard `time.Duration` because,
// to support user-friendly duration format (e.g: "1h30m45s") in JSON value.
type Limits struct {
	// Distributor enforced limits.
	IngestionRateStrategy       string           `yaml:"ingestion_rate_strategy" json:"ingestion_rate_strategy"`
	IngestionRateMB             float64          `yaml:"ingestion_rate_mb" json:"ingestion_rate_mb"`
	IngestionBurstSizeMB        float64          `yaml:"ingestion_burst_size_mb" json:"ingestion_burst_size_mb"`
	MaxLabelNameLength          int              `yaml:"max_label_name_length" json:"max_label_name_length"`
	MaxLabelValueLength         int              `yaml:"max_label_value_length" json:"max_label_value_length"`
	MaxLabelNamesPerSeries      int              `yaml:"max_label_names_per_series" json:"max_label_names_per_series"`
	RejectOldSamples            bool             `yaml:"reject_old_samples" json:"reject_old_samples"`
	RejectOldSamplesMaxAge      model.Duration   `yaml:"reject_old_samples_max_age" json:"reject_old_samples_max_age"`
	CreationGracePeriod         model.Duration   `yaml:"creation_grace_period" json:"creation_grace_period"`
	MaxLineSize                 flagext.ByteSize `yaml:"max_line_size" json:"max_line_size"`
	MaxLineSizeTruncate         bool             `yaml:"max_line_size_truncate" json:"max_line_size_truncate"`
	IncrementDuplicateTimestamp bool             `yaml:"increment_duplicate_timestamp" json:"increment_duplicate_timestamp"`
	SimulatedPushLatency        time.Duration    `yaml:"simulated_push_latency" json:"simulated_push_latency" doc:"description=Simulated latency to add to push requests. Used for testing. Set to 0s to disable."`

	// LogQL engine options
	EnableMultiVariantQueries bool `yaml:"enable_multi_variant_queries" json:"enable_multi_variant_queries"`

	// Metadata field extraction
	DiscoverGenericFields    FieldDetectorConfig `yaml:"discover_generic_fields" json:"discover_generic_fields" doc:"description=Experimental: Detect fields from stream labels, structured metadata, or json/logfmt formatted log line and put them into structured metadata of the log entry."`
	DiscoverServiceName      []string            `yaml:"discover_service_name" json:"discover_service_name"`
	DiscoverLogLevels        bool                `yaml:"discover_log_levels" json:"discover_log_levels"`
	LogLevelFields           []string            `yaml:"log_level_fields" json:"log_level_fields"`
	LogLevelFromJSONMaxDepth int                 `yaml:"log_level_from_json_max_depth" json:"log_level_from_json_max_depth"`

	// Ingester enforced limits.
	UseOwnedStreamCount     bool             `yaml:"use_owned_stream_count" json:"use_owned_stream_count"`
	MaxLocalStreamsPerUser  int              `yaml:"max_streams_per_user" json:"max_streams_per_user"`
	MaxGlobalStreamsPerUser int              `yaml:"max_global_streams_per_user" json:"max_global_streams_per_user"`
	UnorderedWrites         bool             `yaml:"unordered_writes" json:"unordered_writes"`
	PerStreamRateLimit      flagext.ByteSize `yaml:"per_stream_rate_limit" json:"per_stream_rate_limit"`
	PerStreamRateLimitBurst flagext.ByteSize `yaml:"per_stream_rate_limit_burst" json:"per_stream_rate_limit_burst"`

	// Querier enforced limits.
	MaxChunksPerQuery          int              `yaml:"max_chunks_per_query" json:"max_chunks_per_query"`
	MaxQuerySeries             int              `yaml:"max_query_series" json:"max_query_series"`
	MaxQueryLookback           model.Duration   `yaml:"max_query_lookback" json:"max_query_lookback"`
	MaxQueryLength             model.Duration   `yaml:"max_query_length" json:"max_query_length"`
	MaxQueryRange              model.Duration   `yaml:"max_query_range" json:"max_query_range"`
	MaxQueryParallelism        int              `yaml:"max_query_parallelism" json:"max_query_parallelism"`
	TSDBMaxQueryParallelism    int              `yaml:"tsdb_max_query_parallelism" json:"tsdb_max_query_parallelism"`
	TSDBMaxBytesPerShard       flagext.ByteSize `yaml:"tsdb_max_bytes_per_shard" json:"tsdb_max_bytes_per_shard"`
	TSDBShardingStrategy       string           `yaml:"tsdb_sharding_strategy" json:"tsdb_sharding_strategy"`
	TSDBPrecomputeChunks       bool             `yaml:"tsdb_precompute_chunks" json:"tsdb_precompute_chunks"`
	CardinalityLimit           int              `yaml:"cardinality_limit" json:"cardinality_limit"`
	MaxStreamsMatchersPerQuery int              `yaml:"max_streams_matchers_per_query" json:"max_streams_matchers_per_query"`
	MaxConcurrentTailRequests  int              `yaml:"max_concurrent_tail_requests" json:"max_concurrent_tail_requests"`
	MaxEntriesLimitPerQuery    int              `yaml:"max_entries_limit_per_query" json:"max_entries_limit_per_query"`
	MaxCacheFreshness          model.Duration   `yaml:"max_cache_freshness_per_query" json:"max_cache_freshness_per_query"`
	MaxMetadataCacheFreshness  model.Duration   `yaml:"max_metadata_cache_freshness" json:"max_metadata_cache_freshness"`
	MaxStatsCacheFreshness     model.Duration   `yaml:"max_stats_cache_freshness" json:"max_stats_cache_freshness"`
	MaxQueriersPerTenant       uint             `yaml:"max_queriers_per_tenant" json:"max_queriers_per_tenant"`
	MaxQueryCapacity           float64          `yaml:"max_query_capacity" json:"max_query_capacity"`
	QueryReadyIndexNumDays     int              `yaml:"query_ready_index_num_days" json:"query_ready_index_num_days"`
	QueryTimeout               model.Duration   `yaml:"query_timeout" json:"query_timeout"`

	// Query frontend enforced limits. The default is actually parameterized by the queryrange config.
	QuerySplitDuration               model.Duration   `yaml:"split_queries_by_interval" json:"split_queries_by_interval"`
	MetadataQuerySplitDuration       model.Duration   `yaml:"split_metadata_queries_by_interval" json:"split_metadata_queries_by_interval"`
	RecentMetadataQuerySplitDuration model.Duration   `yaml:"split_recent_metadata_queries_by_interval" json:"split_recent_metadata_queries_by_interval"`
	RecentMetadataQueryWindow        model.Duration   `yaml:"recent_metadata_query_window" json:"recent_metadata_query_window"`
	InstantMetricQuerySplitDuration  model.Duration   `yaml:"split_instant_metric_queries_by_interval" json:"split_instant_metric_queries_by_interval"`
	IngesterQuerySplitDuration       model.Duration   `yaml:"split_ingester_queries_by_interval" json:"split_ingester_queries_by_interval"`
	MinShardingLookback              model.Duration   `yaml:"min_sharding_lookback" json:"min_sharding_lookback"`
	MaxQueryBytesRead                flagext.ByteSize `yaml:"max_query_bytes_read" json:"max_query_bytes_read"`
	MaxQuerierBytesRead              flagext.ByteSize `yaml:"max_querier_bytes_read" json:"max_querier_bytes_read"`
	VolumeEnabled                    bool             `yaml:"volume_enabled" json:"volume_enabled" doc:"description=Enable log-volume endpoints."`
	VolumeMaxSeries                  int              `yaml:"volume_max_series" json:"volume_max_series" doc:"description=The maximum number of aggregated series in a log-volume response"`

	// Ruler defaults and limits.
	RulerMaxRulesPerRuleGroup   int                              `yaml:"ruler_max_rules_per_rule_group" json:"ruler_max_rules_per_rule_group"`
	RulerMaxRuleGroupsPerTenant int                              `yaml:"ruler_max_rule_groups_per_tenant" json:"ruler_max_rule_groups_per_tenant"`
	RulerAlertManagerConfig     *ruler_config.AlertManagerConfig `yaml:"ruler_alertmanager_config" json:"ruler_alertmanager_config" doc:"hidden"`
	RulerTenantShardSize        int                              `yaml:"ruler_tenant_shard_size" json:"ruler_tenant_shard_size"`

	// TODO(dannyk): add HTTP client overrides (basic auth / tls config, etc)
	// Ruler remote-write limits.

	// this field is the inversion of the general remote_write.enabled because the zero value of a boolean is false,
	// and if it were ruler_remote_write_enabled, it would be impossible to know if the value was explicitly set or default
	RulerRemoteWriteDisabled bool `yaml:"ruler_remote_write_disabled" json:"ruler_remote_write_disabled" doc:"description=Disable recording rules remote-write."`

	// deprecated use RulerRemoteWriteConfig instead.
	RulerRemoteWriteURL string `yaml:"ruler_remote_write_url" json:"ruler_remote_write_url" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. The URL of the endpoint to send samples to."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteTimeout time.Duration `yaml:"ruler_remote_write_timeout" json:"ruler_remote_write_timeout" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Timeout for requests to the remote write endpoint."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteHeaders OverwriteMarshalingStringMap `yaml:"ruler_remote_write_headers" json:"ruler_remote_write_headers" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Custom HTTP headers to be sent along with each remote write request. Be aware that headers that are set by Loki itself can't be overwritten."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteRelabelConfigs []*util.RelabelConfig `yaml:"ruler_remote_write_relabel_configs,omitempty" json:"ruler_remote_write_relabel_configs,omitempty" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. List of remote write relabel configurations."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueCapacity int `yaml:"ruler_remote_write_queue_capacity" json:"ruler_remote_write_queue_capacity" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Number of samples to buffer per shard before we block reading of more samples from the WAL. It is recommended to have enough capacity in each shard to buffer several requests to keep throughput up while processing occasional slow remote requests."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueMinShards int `yaml:"ruler_remote_write_queue_min_shards" json:"ruler_remote_write_queue_min_shards" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Minimum number of shards, i.e. amount of concurrency."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueMaxShards int `yaml:"ruler_remote_write_queue_max_shards" json:"ruler_remote_write_queue_max_shards" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Maximum number of shards, i.e. amount of concurrency."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueMaxSamplesPerSend int `yaml:"ruler_remote_write_queue_max_samples_per_send" json:"ruler_remote_write_queue_max_samples_per_send" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Maximum number of samples per send."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueBatchSendDeadline time.Duration `yaml:"ruler_remote_write_queue_batch_send_deadline" json:"ruler_remote_write_queue_batch_send_deadline" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Maximum time a sample will wait in buffer."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueMinBackoff time.Duration `yaml:"ruler_remote_write_queue_min_backoff" json:"ruler_remote_write_queue_min_backoff" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Initial retry delay. Gets doubled for every retry."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueMaxBackoff time.Duration `yaml:"ruler_remote_write_queue_max_backoff" json:"ruler_remote_write_queue_max_backoff" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Maximum retry delay."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteQueueRetryOnRateLimit bool `yaml:"ruler_remote_write_queue_retry_on_ratelimit" json:"ruler_remote_write_queue_retry_on_ratelimit" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Retry upon receiving a 429 status code from the remote-write storage. This is experimental and might change in the future."`
	// deprecated use RulerRemoteWriteConfig instead
	RulerRemoteWriteSigV4Config *sigv4.SigV4Config `yaml:"ruler_remote_write_sigv4_config" json:"ruler_remote_write_sigv4_config" doc:"deprecated|description=Use 'ruler_remote_write_config' instead. Configures AWS's Signature Verification 4 signing process to sign every remote write request."`

	RulerRemoteWriteConfig map[string]config.RemoteWriteConfig `yaml:"ruler_remote_write_config,omitempty" json:"ruler_remote_write_config,omitempty" doc:"description=Configures global and per-tenant limits for remote write clients. A map with remote client id as key."`

	// TODO(dannyk): possible enhancement is to align this with rule group interval
	RulerRemoteEvaluationTimeout         time.Duration `yaml:"ruler_remote_evaluation_timeout" json:"ruler_remote_evaluation_timeout" doc:"description=Timeout for a remote rule evaluation. Defaults to the value of 'querier.query-timeout'."`
	RulerRemoteEvaluationMaxResponseSize int64         `yaml:"ruler_remote_evaluation_max_response_size" json:"ruler_remote_evaluation_max_response_size" doc:"description=Maximum size (in bytes) of the allowable response size from a remote rule evaluation. Set to 0 to allow any response size (default)."`

	// Global and per tenant deletion mode
	DeletionMode string `yaml:"deletion_mode" json:"deletion_mode"`

	// Global and per tenant retention
	RetentionPeriod model.Duration    `yaml:"retention_period" json:"retention_period"`
	StreamRetention []StreamRetention `yaml:"retention_stream,omitempty" json:"retention_stream,omitempty" doc:"description=Per-stream retention to apply, if the retention is enabled on the compactor side.\nExample:\n retention_stream:\n - selector: '{namespace=\"dev\"}'\n priority: 1\n period: 24h\n- selector: '{container=\"nginx\"}'\n priority: 1\n period: 744h\nSelector is a Prometheus labels matchers that will apply the 'period' retention only if the stream is matching. In case multiple streams are matching, the highest priority will be picked. If no rule is matched the 'retention_period' is used."`

	// Config for overrides, convenient if it goes here.
	PerTenantOverrideConfig string         `yaml:"per_tenant_override_config" json:"per_tenant_override_config"`
	PerTenantOverridePeriod model.Duration `yaml:"per_tenant_override_period" json:"per_tenant_override_period"`

	// Deprecated
	CompactorDeletionEnabled bool `yaml:"allow_deletes" json:"allow_deletes" doc:"deprecated|description=Use deletion_mode per tenant configuration instead."`

	ShardStreams shardstreams.Config `yaml:"shard_streams" json:"shard_streams" doc:"description=Define streams sharding behavior."`

	BlockedQueries []*validation.BlockedQuery `yaml:"blocked_queries,omitempty" json:"blocked_queries,omitempty"`

	RequiredLabels       []string `yaml:"required_labels,omitempty" json:"required_labels,omitempty" doc:"description=Define a list of required selector labels."`
	RequiredNumberLabels int      `yaml:"minimum_labels_number,omitempty" json:"minimum_labels_number,omitempty" doc:"description=Minimum number of label matchers a query should contain."`

	IndexGatewayShardSize int `yaml:"index_gateway_shard_size" json:"index_gateway_shard_size"`

	BloomGatewayEnabled bool `yaml:"bloom_gateway_enable_filtering" json:"bloom_gateway_enable_filtering" category:"experimental"`

	BloomBuildMaxBuilders       int           `yaml:"bloom_build_max_builders" json:"bloom_build_max_builders" category:"experimental"`
	BloomBuildTaskMaxRetries    int           `yaml:"bloom_build_task_max_retries" json:"bloom_build_task_max_retries" category:"experimental"`
	BloomBuilderResponseTimeout time.Duration `yaml:"bloom_build_builder_response_timeout" json:"bloom_build_builder_response_timeout" category:"experimental"`

	BloomCreationEnabled           bool             `yaml:"bloom_creation_enabled" json:"bloom_creation_enabled" category:"experimental"`
	BloomPlanningStrategy          string           `yaml:"bloom_planning_strategy" json:"bloom_planning_strategy" category:"experimental"`
	BloomSplitSeriesKeyspaceBy     int              `yaml:"bloom_split_series_keyspace_by" json:"bloom_split_series_keyspace_by" category:"experimental"`
	BloomTaskTargetSeriesChunkSize flagext.ByteSize `yaml:"bloom_task_target_series_chunk_size" json:"bloom_task_target_series_chunk_size" category:"experimental"`
	BloomBlockEncoding             string           `yaml:"bloom_block_encoding" json:"bloom_block_encoding" category:"experimental"`
	BloomPrefetchBlocks            bool             `yaml:"bloom_prefetch_blocks" json:"bloom_prefetch_blocks" category:"experimental"`

	BloomMaxBlockSize flagext.ByteSize `yaml:"bloom_max_block_size" json:"bloom_max_block_size" category:"experimental"`
	BloomMaxBloomSize flagext.ByteSize `yaml:"bloom_max_bloom_size" json:"bloom_max_bloom_size" category:"experimental"`

	AllowStructuredMetadata           bool                  `yaml:"allow_structured_metadata,omitempty" json:"allow_structured_metadata,omitempty" doc:"description=Allow user to send structured metadata in push payload."`
	MaxStructuredMetadataSize         flagext.ByteSize      `yaml:"max_structured_metadata_size" json:"max_structured_metadata_size" doc:"description=Maximum size accepted for structured metadata per log line."`
	MaxStructuredMetadataEntriesCount int                   `yaml:"max_structured_metadata_entries_count" json:"max_structured_metadata_entries_count" doc:"description=Maximum number of structured metadata entries per log line."`
	OTLPConfig                        push.OTLPConfig       `yaml:"otlp_config" json:"otlp_config" doc:"description=OTLP log ingestion configurations"`
	GlobalOTLPConfig                  push.GlobalOTLPConfig `yaml:"-" json:"-"`

	BlockIngestionPolicyUntil map[string]dskit_flagext.Time `yaml:"block_ingestion_policy_until" json:"block_ingestion_policy_until" category:"experimental" doc:"description=Block ingestion for policy until the configured date. The policy '*' is the global policy, which is applied to all streams not matching a policy and can be overridden by other policies. The time should be in RFC3339 format. The policy is based on the policy_stream_mapping configuration."`
	BlockIngestionUntil       dskit_flagext.Time            `yaml:"block_ingestion_until" json:"block_ingestion_until" category:"experimental"`
	BlockIngestionStatusCode  int                           `yaml:"block_ingestion_status_code" json:"block_ingestion_status_code"`
	EnforcedLabels            []string                      `yaml:"enforced_labels" json:"enforced_labels" category:"experimental"`
	PolicyEnforcedLabels      map[string][]string           `yaml:"policy_enforced_labels" json:"policy_enforced_labels" category:"experimental" doc:"description=Map of policies to enforced labels. The policy '*' is the global policy, which is applied to all streams and can be extended by other policies. Example:\n policy_enforced_labels: \n  policy1: \n    - label1 \n    - label2 \n  policy2: \n    - label3 \n    - label4\n  '*':\n    - label5"`
	PolicyStreamMapping       PolicyStreamMapping           `yaml:"policy_stream_mapping" json:"policy_stream_mapping" category:"experimental" doc:"description=Map of policies to stream selectors with a priority. Experimental.  Example:\n policy_stream_mapping: \n  finance: \n    - selector: '{namespace=\"prod\", container=\"billing\"}' \n      priority: 2 \n  ops: \n    - selector: '{namespace=\"prod\", container=\"ops\"}' \n      priority: 1 \n  staging: \n    - selector: '{namespace=\"staging\"}' \n      priority: 1"`

	IngestionPartitionsTenantShardSize int `yaml:"ingestion_partitions_tenant_shard_size" json:"ingestion_partitions_tenant_shard_size" category:"experimental"`

	ShardAggregations []string `yaml:"shard_aggregations,omitempty" json:"shard_aggregations,omitempty" doc:"description=List of LogQL vector and range aggregations that should be sharded."`

	PatternIngesterTokenizableJSONFieldsDefault dskit_flagext.StringSliceCSV `yaml:"pattern_ingester_tokenizable_json_fields_default" json:"pattern_ingester_tokenizable_json_fields_default" doc:"hidden"`
	PatternIngesterTokenizableJSONFieldsAppend  dskit_flagext.StringSliceCSV `yaml:"pattern_ingester_tokenizable_json_fields_append"  json:"pattern_ingester_tokenizable_json_fields_append"  doc:"hidden"`
	PatternIngesterTokenizableJSONFieldsDelete  dskit_flagext.StringSliceCSV `yaml:"pattern_ingester_tokenizable_json_fields_delete"  json:"pattern_ingester_tokenizable_json_fields_delete"  doc:"hidden"`
	MetricAggregationEnabled                    bool                         `yaml:"metric_aggregation_enabled"                       json:"metric_aggregation_enabled"`

	// This config doesn't have a CLI flag registered here because they're registered in
	// their own original config struct.
	S3SSEType                 string `yaml:"s3_sse_type" json:"s3_sse_type" doc:"nocli|description=S3 server-side encryption type. Required to enable server-side encryption overrides for a specific tenant. If not set, the default S3 client settings are used."`
	S3SSEKMSKeyID             string `yaml:"s3_sse_kms_key_id" json:"s3_sse_kms_key_id" doc:"nocli|description=S3 server-side encryption KMS Key ID. Ignored if the SSE type override is not set."`
	S3SSEKMSEncryptionContext string `yaml:"s3_sse_kms_encryption_context" json:"s3_sse_kms_encryption_context" doc:"nocli|description=S3 server-side encryption KMS encryption context. If unset and the key ID override is set, the encryption context will not be provided to S3. Ignored if the SSE type override is not set."`
}

type FieldDetectorConfig struct {
	Fields map[string][]string `yaml:"fields,omitempty" json:"fields,omitempty"`
}

type StreamRetention struct {
	Period   model.Duration    `yaml:"period" json:"period" doc:"description:Retention period applied to the log lines matching the selector."`
	Priority int               `yaml:"priority" json:"priority" doc:"description:The larger the value, the higher the priority."`
	Selector string            `yaml:"selector" json:"selector" doc:"description:Stream selector expression."`
	Matchers []*labels.Matcher `yaml:"-" json:"-"` // populated during validation.
}

// LimitError are errors that do not comply with the limits specified.
type LimitError string

func (e LimitError) Error() string {
	return string(e)
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (l *Limits) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&l.IngestionRateStrategy, "distributor.ingestion-rate-limit-strategy", "global", "Whether the ingestion rate limit should be applied individually to each distributor instance (local), or evenly shared across the cluster (global). The ingestion rate strategy cannot be overridden on a per-tenant basis.\n- local: enforces the limit on a per distributor basis. The actual effective rate limit will be N times higher, where N is the number of distributor replicas.\n- global: enforces the limit globally, configuring a per-distributor local rate limiter as 'ingestion_rate / N', where N is the number of distributor replicas (it's automatically adjusted if the number of replicas change). The global strategy requires the distributors to form their own ring, which is used to keep track of the current number of healthy distributor replicas.")
	f.Float64Var(&l.IngestionRateMB, "distributor.ingestion-rate-limit-mb", 4, "Per-user ingestion rate limit in sample size per second. Sample size includes size of the logs line and the size of structured metadata labels. Units in MB.")
	f.Float64Var(&l.IngestionBurstSizeMB, "distributor.ingestion-burst-size-mb", 6, "Per-user allowed ingestion burst size (in sample size). Units in MB. The burst size refers to the per-distributor local rate limiter even in the case of the 'global' strategy, and should be set at least to the maximum logs size expected in a single push request.")

	_ = l.MaxLineSize.Set("256KB")
	f.Var(&l.MaxLineSize, "distributor.max-line-size", "Maximum line size on ingestion path. Example: 256kb. Any log line exceeding this limit will be discarded unless `distributor.max-line-size-truncate` is set which in case it is truncated instead of discarding it completely. There is no limit when unset or set to 0.")
	f.BoolVar(&l.MaxLineSizeTruncate, "distributor.max-line-size-truncate", false, "Whether to truncate lines that exceed max_line_size.")
	f.IntVar(&l.MaxLabelNameLength, "validation.max-length-label-name", 1024, "Maximum length accepted for label names.")
	f.IntVar(&l.MaxLabelValueLength, "validation.max-length-label-value", 2048, "Maximum length accepted for label value. This setting also applies to the metric name.")
	f.IntVar(&l.MaxLabelNamesPerSeries, "validation.max-label-names-per-series", 15, "Maximum number of label names per series.")
	f.BoolVar(&l.RejectOldSamples, "validation.reject-old-samples", true, "Whether or not old samples will be rejected.")
	f.BoolVar(&l.IncrementDuplicateTimestamp, "validation.increment-duplicate-timestamps", false, "Alter the log line timestamp during ingestion when the timestamp is the same as the previous entry for the same stream. When enabled, if a log line in a push request has the same timestamp as the previous line for the same stream, one nanosecond is added to the log line. This will preserve the received order of log lines with the exact same timestamp when they are queried, by slightly altering their stored timestamp. NOTE: This is imperfect, because Loki accepts out of order writes, and another push request for the same stream could contain duplicate timestamps to existing entries and they will not be incremented.")
	l.DiscoverServiceName = []string{
		"service",
		"app",
		"application",
		"app_name",
		"name",
		"app_kubernetes_io_name",
		"container",
		"container_name",
		"k8s_container_name",
		"component",
		"workload",
		"job",
		"k8s_job_name",
	}
	f.Var((*dskit_flagext.StringSlice)(&l.DiscoverServiceName), "validation.discover-service-name", "If no service_name label exists, Loki maps a single label from the configured list to service_name. If none of the configured labels exist in the stream, label is set to unknown_service. Empty list disables setting the label.")
	f.BoolVar(&l.DiscoverLogLevels, "validation.discover-log-levels", true, "Discover and add log levels during ingestion, if not present already. Levels would be added to Structured Metadata with name level/LEVEL/Level/Severity/severity/SEVERITY/lvl/LVL/Lvl (case-sensitive) and one of the values from 'trace', 'debug', 'info', 'warn', 'error', 'critical', 'fatal' (case insensitive).")
	l.LogLevelFields = []string{"level", "LEVEL", "Level", "Severity", "severity", "SEVERITY", "lvl", "LVL", "Lvl"}
	f.Var((*dskit_flagext.StringSlice)(&l.LogLevelFields), "validation.log-level-fields", "Field name to use for log levels. If not set, log level would be detected based on pre-defined labels as mentioned above.")
	f.IntVar(&l.LogLevelFromJSONMaxDepth, "validation.log-level-from-json-max-depth", 2, "Maximum depth to search for log level fields in JSON logs. A value of 0 or less means unlimited depth. Default is 2 which searches the first 2 levels of the JSON object.")

	_ = l.RejectOldSamplesMaxAge.Set("7d")
	f.Var(&l.RejectOldSamplesMaxAge, "validation.reject-old-samples.max-age", "Maximum accepted sample age before rejecting.")
	_ = l.CreationGracePeriod.Set("10m")
	f.Var(&l.CreationGracePeriod, "validation.create-grace-period", "Duration which table will be created/deleted before/after it's needed; we won't accept sample from before this time.")
	f.IntVar(&l.MaxEntriesLimitPerQuery, "validation.max-entries-limit", 5000, "Maximum number of log entries that will be returned for a query.")

	f.BoolVar(&l.UseOwnedStreamCount, "ingester.use-owned-stream-count", false, "When true an ingester takes into account only the streams that it owns according to the ring while applying the stream limit.")
	f.IntVar(&l.MaxLocalStreamsPerUser, "ingester.max-streams-per-user", 0, "Maximum number of active streams per user, per ingester. 0 to disable.")
	f.IntVar(&l.MaxGlobalStreamsPerUser, "ingester.max-global-streams-per-user", 5000, "Maximum number of active streams per user, across the cluster. 0 to disable. When the global limit is enabled, each ingester is configured with a dynamic local limit based on the replication factor and the current number of healthy ingesters, and is kept updated whenever the number of ingesters change.")

	// TODO(ashwanth) Deprecated. This will be removed with the next major release and out-of-order writes would be accepted by default.
	f.BoolVar(&l.UnorderedWrites, "ingester.unordered-writes", true, "Deprecated. When true, out-of-order writes are accepted.")

	_ = l.PerStreamRateLimit.Set(strconv.Itoa(defaultPerStreamRateLimit))
	f.Var(&l.PerStreamRateLimit, "ingester.per-stream-rate-limit", "Maximum byte rate per second per stream, also expressible in human readable forms (1MB, 256KB, etc).")
	_ = l.PerStreamRateLimitBurst.Set(strconv.Itoa(defaultPerStreamBurstLimit))
	f.Var(&l.PerStreamRateLimitBurst, "ingester.per-stream-rate-limit-burst", "Maximum burst bytes per stream, also expressible in human readable forms (1MB, 256KB, etc). This is how far above the rate limit a stream can 'burst' before the stream is limited.")

	f.IntVar(&l.MaxChunksPerQuery, "store.query-chunk-limit", 2e6, "Maximum number of chunks that can be fetched in a single query.")

	_ = l.MaxQueryLength.Set("721h")
	f.Var(&l.MaxQueryLength, "store.max-query-length", "The limit to length of chunk store queries. 0 to disable.")
	f.IntVar(&l.MaxQuerySeries, "querier.max-query-series", 500, "Limit the maximum of unique series that is returned by a metric query. When the limit is reached an error is returned.")
	_ = l.MaxQueryRange.Set("0s")
	f.Var(&l.MaxQueryRange, "querier.max-query-range", "Limit the length of the [range] inside a range query. Default is 0 or unlimited")
	_ = l.QueryTimeout.Set(DefaultPerTenantQueryTimeout)
	f.Var(&l.QueryTimeout, "querier.query-timeout", "Timeout when querying backends (ingesters or storage) during the execution of a query request. When a specific per-tenant timeout is used, the global timeout is ignored.")

	_ = l.MaxQueryLookback.Set("0s")
	f.Var(&l.MaxQueryLookback, "querier.max-query-lookback", "Limit how far back in time series data and metadata can be queried, up until lookback duration ago. This limit is enforced in the query frontend, the querier and the ruler. If the requested time range is outside the allowed range, the request will not fail, but will be modified to only query data within the allowed time range. The default value of 0 does not set a limit.")
	f.IntVar(&l.MaxQueryParallelism, "querier.max-query-parallelism", 32, "Maximum number of queries that will be scheduled in parallel by the frontend.")
	f.IntVar(&l.TSDBMaxQueryParallelism, "querier.tsdb-max-query-parallelism", 128, "Maximum number of queries will be scheduled in parallel by the frontend for TSDB schemas.")
	_ = l.TSDBMaxBytesPerShard.Set(strconv.Itoa(DefaultTSDBMaxBytesPerShard))
	f.Var(&l.TSDBMaxBytesPerShard, "querier.tsdb-max-bytes-per-shard", "Target maximum number of bytes assigned to a single sharded query. Also expressible in human readable forms (1GB, etc). Note: This is a _target_ and not an absolute limit. The actual limit can be higher, but the query planner will try to build shards up to this limit.")
	f.StringVar(
		&l.TSDBShardingStrategy,
		"limits.tsdb-sharding-strategy",
		logql.PowerOfTwoVersion.String(),
		fmt.Sprintf(
			"sharding strategy to use in query planning. Suggested to use %s once all nodes can recognize it.",
			logql.BoundedVersion.String(),
		),
	)
	f.BoolVar(&l.TSDBPrecomputeChunks, "querier.tsdb-precompute-chunks", false, "Precompute chunks for TSDB queries. This can improve query performance at the cost of increased memory usage by computing chunks once during planning, reducing index calls.")
	f.IntVar(&l.CardinalityLimit, "store.cardinality-limit", 1e5, "Cardinality limit for index queries.")
	f.IntVar(&l.MaxStreamsMatchersPerQuery, "querier.max-streams-matcher-per-query", 1000, "Maximum number of stream matchers per query.")
	f.IntVar(&l.MaxConcurrentTailRequests, "querier.max-concurrent-tail-requests", 10, "Maximum number of concurrent tail requests.")

	_ = l.MinShardingLookback.Set("0s")
	f.Var(&l.MinShardingLookback, "frontend.min-sharding-lookback", "Limit queries that can be sharded. Queries within the time range of now and now minus this sharding lookback are not sharded. The default value of 0s disables the lookback, causing sharding of all queries at all times.")

	f.Var(&l.MaxQueryBytesRead, "frontend.max-query-bytes-read", "Max number of bytes a query can fetch. Enforced in log and metric queries only when TSDB is used. This limit is not enforced on log queries without filters. The default value of 0 disables this limit.")

	_ = l.MaxQuerierBytesRead.Set("150GB")
	f.Var(&l.MaxQuerierBytesRead, "frontend.max-querier-bytes-read", "Max number of bytes a query can fetch after splitting and sharding. Enforced in log and metric queries only when TSDB is used. This limit is not enforced on log queries without filters. The default value of 0 disables this limit.")

	_ = l.MaxCacheFreshness.Set("10m")
	f.Var(&l.MaxCacheFreshness, "frontend.max-cache-freshness", "Most recent allowed cacheable result per-tenant, to prevent caching very recent results that might still be in flux.")

	_ = l.MaxMetadataCacheFreshness.Set("24h")
	f.Var(&l.MaxMetadataCacheFreshness, "frontend.max-metadata-cache-freshness", "Do not cache metadata request if the end time is within the frontend.max-metadata-cache-freshness window. Set this to 0 to apply no such limits. Defaults to 24h.")

	_ = l.MaxStatsCacheFreshness.Set("10m")
	f.Var(&l.MaxStatsCacheFreshness, "frontend.max-stats-cache-freshness", "Do not cache requests with an end time that falls within Now minus this duration. 0 disables this feature (default).")

	f.UintVar(&l.MaxQueriersPerTenant, "frontend.max-queriers-per-tenant", 0, "Maximum number of queriers that can handle requests for a single tenant. If set to 0 or value higher than number of available queriers, *all* queriers will handle requests for the tenant. Each frontend (or query-scheduler, if used) will select the same set of queriers for the same tenant (given that all queriers are connected to all frontends / query-schedulers). This option only works with queriers connecting to the query-frontend / query-scheduler, not when using downstream URL.")
	f.Float64Var(&l.MaxQueryCapacity, "frontend.max-query-capacity", 0, "How much of the available query capacity (\"querier\" components in distributed mode, \"read\" components in SSD mode) can be used by a single tenant. Allowed values are 0.0 to 1.0. For example, setting this to 0.5 would allow a tenant to use half of the available queriers for processing the query workload. If set to 0, query capacity is determined by frontend.max-queriers-per-tenant. When both frontend.max-queriers-per-tenant and frontend.max-query-capacity are configured, smaller value of the resulting querier replica count is considered: min(frontend.max-queriers-per-tenant, ceil(querier_replicas * frontend.max-query-capacity)). *All* queriers will handle requests for the tenant if neither limits are applied. This option only works with queriers connecting to the query-frontend / query-scheduler, not when using downstream URL. Use this feature in a multi-tenant setup where you need to limit query capacity for certain tenants.")
	f.IntVar(&l.QueryReadyIndexNumDays, "store.query-ready-index-num-days", 0, "Number of days of index to be kept always downloaded for queries. Applies only to per user index in boltdb-shipper index store. 0 to disable.")

	f.IntVar(&l.RulerMaxRulesPerRuleGroup, "ruler.max-rules-per-rule-group", 0, "Maximum number of rules per rule group per-tenant. 0 to disable.")
	f.IntVar(&l.RulerMaxRuleGroupsPerTenant, "ruler.max-rule-groups-per-tenant", 0, "Maximum number of rule groups per-tenant. 0 to disable.")
	f.IntVar(&l.RulerTenantShardSize, "ruler.tenant-shard-size", 0, "The default tenant's shard size when shuffle-sharding is enabled in the ruler. When this setting is specified in the per-tenant overrides, a value of 0 disables shuffle sharding for the tenant.")

	f.StringVar(&l.PerTenantOverrideConfig, "limits.per-user-override-config", "", "Feature renamed to 'runtime configuration', flag deprecated in favor of -runtime-config.file (runtime_config.file in YAML).")
	_ = l.RetentionPeriod.Set("0s")
	f.Var(&l.RetentionPeriod, "store.retention", "Retention period to apply to stored data, only applies if retention_enabled is true in the compactor config. As of version 2.8.0, a zero value of 0 or 0s disables retention. In previous releases, Loki did not properly honor a zero value to disable retention and a really large value should be used instead.")

	_ = l.PerTenantOverridePeriod.Set("10s")
	f.Var(&l.PerTenantOverridePeriod, "limits.per-user-override-period", "Feature renamed to 'runtime configuration'; flag deprecated in favor of -runtime-config.reload-period (runtime_config.period in YAML).")

	_ = l.QuerySplitDuration.Set("1h")
	f.Var(&l.QuerySplitDuration, "querier.split-queries-by-interval", "Split queries by a time interval and execute in parallel. The value 0 disables splitting by time. This also determines how cache keys are chosen when result caching is enabled.")
	_ = l.InstantMetricQuerySplitDuration.Set("1h")
	f.Var(&l.InstantMetricQuerySplitDuration, "querier.split-instant-metric-queries-by-interval", "Split instant metric queries by a time interval and execute in parallel. The value 0 disables splitting instant metric queries by time. This also determines how cache keys are chosen when instant metric query result caching is enabled.")

	_ = l.MetadataQuerySplitDuration.Set("24h")
	f.Var(&l.MetadataQuerySplitDuration, "querier.split-metadata-queries-by-interval", "Split metadata queries by a time interval and execute in parallel. The value 0 disables splitting metadata queries by time. This also determines how cache keys are chosen when label/series result caching is enabled.")

	_ = l.RecentMetadataQuerySplitDuration.Set("1h")
	f.Var(&l.RecentMetadataQuerySplitDuration, "experimental.querier.split-recent-metadata-queries-by-interval", "Experimental. Split interval to use for the portion of metadata request that falls within `recent_metadata_query_window`. Rest of the request which is outside the window still uses `split_metadata_queries_by_interval`. If set to 0, the entire request defaults to using a split interval of `split_metadata_queries_by_interval.`.")

	f.Var(&l.RecentMetadataQueryWindow, "experimental.querier.recent-metadata-query-window", "Experimental. Metadata query window inside which `split_recent_metadata_queries_by_interval` gets applied, portion of the metadata request that falls in this window is split using `split_recent_metadata_queries_by_interval`. The value 0 disables using a different split interval for recent metadata queries.\n\nThis is added to improve cacheability of recent metadata queries. Query split interval also determines the interval used in cache key. The default split interval of 24h is useful for caching long queries, each cache key holding 1 day's results. But metadata queries are often shorter than 24h, to cache them effectively we need a smaller split interval. `recent_metadata_query_window` along with `split_recent_metadata_queries_by_interval` help configure a shorter split interval for recent metadata queries.")

	_ = l.IngesterQuerySplitDuration.Set("0s")
	f.Var(&l.IngesterQuerySplitDuration, "querier.split-ingester-queries-by-interval", "Interval to use for time-based splitting when a request is within the `query_ingesters_within` window; defaults to `split-queries-by-interval` by setting to 0.")

	f.StringVar(&l.DeletionMode, "compactor.deletion-mode", "filter-and-delete", "Deletion mode. Can be one of 'disabled', 'filter-only', or 'filter-and-delete'. When set to 'filter-only' or 'filter-and-delete', and if retention_enabled is true, then the log entry deletion API endpoints are available.")

	// Deprecated
	dskit_flagext.DeprecatedFlag(f, "compactor.allow-deletes", "Deprecated. Instead, see compactor.deletion-mode which is another per tenant configuration", util_log.Logger)

	f.IntVar(&l.IndexGatewayShardSize, "index-gateway.shard-size", 0, "The shard size defines how many index gateways should be used by a tenant for querying. If the global shard factor is 0, the global shard factor is set to the deprecated -replication-factor for backwards compatibility reasons.")

	f.BoolVar(&l.BloomGatewayEnabled, "bloom-gateway.enable-filtering", false, "Experimental. Whether to use the bloom gateway component in the read path to filter chunks.")

	f.StringVar(&l.BloomBlockEncoding, "bloom-build.block-encoding", "none", "Experimental. Compression algorithm for bloom block pages.")
	f.BoolVar(&l.BloomPrefetchBlocks, "bloom-build.prefetch-blocks", false, "Experimental. Prefetch blocks on bloom gateways as soon as they are built.")

	_ = l.BloomMaxBlockSize.Set(defaultBloomBuildMaxBlockSize)
	f.Var(&l.BloomMaxBlockSize, "bloom-build.max-block-size",
		fmt.Sprintf(
			"Experimental. The maximum bloom block size. A value of 0 sets an unlimited size. Default is %s. The actual block size might exceed this limit since blooms will be added to blocks until the block exceeds the maximum block size.",
			defaultBloomBuildMaxBlockSize,
		),
	)

	f.BoolVar(&l.BloomCreationEnabled, "bloom-build.enable", false, "Experimental. Whether to create blooms for the tenant.")
	f.StringVar(&l.BloomPlanningStrategy, "bloom-build.planning-strategy", "split_keyspace_by_factor", "Experimental. Bloom planning strategy to use in bloom creation. Can be one of: 'split_keyspace_by_factor', 'split_by_series_chunks_size'")
	f.IntVar(&l.BloomSplitSeriesKeyspaceBy, "bloom-build.split-keyspace-by", 256, "Experimental. Only if `bloom-build.planning-strategy` is 'split'. Number of splits to create for the series keyspace when building blooms. The series keyspace is split into this many parts to parallelize bloom creation.")
	_ = l.BloomTaskTargetSeriesChunkSize.Set(defaultBloomTaskTargetChunkSize)
	f.Var(&l.BloomTaskTargetSeriesChunkSize, "bloom-build.split-target-series-chunk-size", fmt.Sprintf("Experimental. Target chunk size in bytes for bloom tasks. Default is %s.", defaultBloomTaskTargetChunkSize))
	f.IntVar(&l.BloomBuildMaxBuilders, "bloom-build.max-builders", 0, "Experimental. Maximum number of builders to use when building blooms. 0 allows unlimited builders.")
	f.DurationVar(&l.BloomBuilderResponseTimeout, "bloom-build.builder-response-timeout", 0, "Experimental. Timeout for a builder to finish a task. If a builder does not respond within this time, it is considered failed and the task will be requeued. 0 disables the timeout.")
	f.IntVar(&l.BloomBuildTaskMaxRetries, "bloom-build.task-max-retries", 3, "Experimental. Maximum number of retries for a failed task. If a task fails more than this number of times, it is considered failed and will not be retried. A value of 0 disables this limit.")

	_ = l.BloomMaxBloomSize.Set(defaultBloomBuildMaxBloomSize)
	f.Var(&l.BloomMaxBloomSize, "bloom-build.max-bloom-size",
		fmt.Sprintf(
			"Experimental. The maximum bloom size per log stream. A log stream whose generated bloom filter exceeds this size will be discarded. A value of 0 sets an unlimited size. Default is %s.",
			defaultBloomBuildMaxBloomSize,
		),
	)

	l.ShardStreams.RegisterFlagsWithPrefix("shard-streams", f)
	f.IntVar(&l.VolumeMaxSeries, "limits.volume-max-series", 1000, "The default number of aggregated series or labels that can be returned from a log-volume endpoint")

	f.BoolVar(&l.AllowStructuredMetadata, "validation.allow-structured-metadata", true, "Allow user to send structured metadata (non-indexed labels) in push payload.")
	_ = l.MaxStructuredMetadataSize.Set(defaultMaxStructuredMetadataSize)
	f.Var(&l.MaxStructuredMetadataSize, "limits.max-structured-metadata-size", "Maximum size accepted for structured metadata per entry. Default: 64 kb. Any log line exceeding this limit will be discarded. There is no limit when unset or set to 0.")
	f.IntVar(&l.MaxStructuredMetadataEntriesCount, "limits.max-structured-metadata-entries-count", defaultMaxStructuredMetadataCount, "Maximum number of structured metadata entries per log line. Default: 128. Any log line exceeding this limit will be discarded. There is no limit when unset or set to 0.")
	f.BoolVar(&l.VolumeEnabled, "limits.volume-enabled", true, "Enable log volume endpoint.")

	f.Var(&l.BlockIngestionUntil, "limits.block-ingestion-until", "Block ingestion until the configured date. The time should be in RFC3339 format.")
	f.IntVar(&l.BlockIngestionStatusCode, "limits.block-ingestion-status-code", defaultBlockedIngestionStatusCode, "HTTP status code to return when ingestion is blocked. If 200, the ingestion will be blocked without returning an error to the client. By Default, a custom status code (260) is returned to the client along with an error message.")
	f.Var((*dskit_flagext.StringSlice)(&l.EnforcedLabels), "validation.enforced-labels", "List of labels that must be present in the stream. If any of the labels are missing, the stream will be discarded. This flag configures it globally for all tenants. Experimental.")
	l.PolicyEnforcedLabels = make(map[string][]string)

	f.IntVar(&l.IngestionPartitionsTenantShardSize, "limits.ingestion-partition-tenant-shard-size", 0, "The number of partitions a tenant's data should be sharded to when using kafka ingestion. Tenants are sharded across partitions using shuffle-sharding. 0 disables shuffle sharding and tenant is sharded across all partitions.")

	_ = l.PatternIngesterTokenizableJSONFieldsDefault.Set("log,message,msg,msg_,_msg,content")
	f.Var(&l.PatternIngesterTokenizableJSONFieldsDefault, "limits.pattern-ingester-tokenizable-json-fields", "List of JSON fields that should be tokenized in the pattern ingester.")
	f.Var(&l.PatternIngesterTokenizableJSONFieldsAppend, "limits.pattern-ingester-tokenizable-json-fields-append", "List of JSON fields that should be appended to the default list of tokenizable fields in the pattern ingester.")
	f.Var(&l.PatternIngesterTokenizableJSONFieldsDelete, "limits.pattern-ingester-tokenizable-json-fields-delete", "List of JSON fields that should be deleted from the (default U append) list of tokenizable fields in the pattern ingester.")

	f.BoolVar(
		&l.MetricAggregationEnabled,
		"limits.metric-aggregation-enabled",
		false,
		"Enable metric aggregation. When enabled, pushed streams will be sampled for bytes and count, and these metric will be written back into Loki as a special __aggregated_metric__ stream, which can be queried for faster histogram queries.",
	)
	f.DurationVar(&l.SimulatedPushLatency, "limits.simulated-push-latency", 0, "Simulated latency to add to push requests. This is used to test the performance of the write path under different latency conditions.")

	f.BoolVar(
		&l.EnableMultiVariantQueries,
		"limits.enable-multi-variant-queries",
		false,
		"Enable experimental support for running multiple query variants over the same underlying data. For example, running both a rate() and count_over_time() query over the same range selector.",
	)
}

// SetGlobalOTLPConfig set GlobalOTLPConfig which is used while unmarshaling per-tenant otlp config to use the default list of resource attributes picked as index labels.
func (l *Limits) SetGlobalOTLPConfig(cfg push.GlobalOTLPConfig) {
	l.GlobalOTLPConfig = cfg
	l.OTLPConfig.ApplyGlobalOTLPConfig(cfg)
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
	if err := unmarshal((*plain)(l)); err != nil {
		return err
	}

	if defaultLimits != nil {
		// apply relevant bits from global otlp config
		l.OTLPConfig.ApplyGlobalOTLPConfig(defaultLimits.GlobalOTLPConfig)
	}
	return nil
}

// Validate validates that this limits config is valid.
func (l *Limits) Validate() error {
	if l.StreamRetention != nil {
		for i, rule := range l.StreamRetention {
			matchers, err := syntax.ParseMatchers(rule.Selector, true)
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

	if l.PolicyStreamMapping != nil {
		if err := l.PolicyStreamMapping.Validate(); err != nil {
			return err
		}
	}

	if _, err := deletionmode.ParseMode(l.DeletionMode); err != nil {
		return err
	}

	if l.CompactorDeletionEnabled {
		level.Warn(util_log.Logger).Log("msg", "The compactor.allow-deletes configuration option has been deprecated and will be ignored. Instead, use deletion_mode in the limits_configs to adjust deletion functionality")
	}

	if l.MaxQueryCapacity < 0 {
		level.Warn(util_log.Logger).Log("msg", "setting frontend.max-query-capacity to 0 as it is configured to a value less than 0")
		l.MaxQueryCapacity = 0
	}

	if l.MaxQueryCapacity > 1 {
		level.Warn(util_log.Logger).Log("msg", "setting frontend.max-query-capacity to 1 as it is configured to a value greater than 1")
		l.MaxQueryCapacity = 1
	}

	if err := l.OTLPConfig.Validate(); err != nil {
		return err
	}

	if _, err := logql.ParseShardVersion(l.TSDBShardingStrategy); err != nil {
		return errors.Wrap(err, "invalid tsdb sharding strategy")
	}

	if _, err := compression.ParseCodec(l.BloomBlockEncoding); err != nil {
		return err
	}

	if l.TSDBMaxBytesPerShard <= 0 {
		return errors.New("querier.tsdb-max-bytes-per-shard must be greater than 0")
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

func (o *Overrides) UseOwnedStreamCount(userID string) bool {
	return o.getOverridesForUser(userID).UseOwnedStreamCount
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
func (o *Overrides) MaxQueryLength(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxQueryLength)
}

// Compatibility with Cortex interface, this method is set to be removed in 1.12,
// so nooping in Loki until then.
func (o *Overrides) MaxChunksPerQueryFromStore(_ string) int { return 0 }

// MaxQuerySeries returns the limit of the series of metric queries.
func (o *Overrides) MaxQuerySeries(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).MaxQuerySeries
}

// MaxQueryRange returns the limit for the max [range] value that can be in a range query
func (o *Overrides) MaxQueryRange(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxQueryRange)
}

// MaxQueriersPerUser returns the maximum number of queriers that can handle requests for this user.
func (o *Overrides) MaxQueriersPerUser(userID string) uint {
	return o.getOverridesForUser(userID).MaxQueriersPerTenant
}

// MaxQueryCapacity returns how much of the available query capacity can be used by this user..
func (o *Overrides) MaxQueryCapacity(userID string) float64 {
	return o.getOverridesForUser(userID).MaxQueryCapacity
}

// QueryReadyIndexNumDays returns the number of days for which we have to be query ready for a user.
func (o *Overrides) QueryReadyIndexNumDays(userID string) int {
	return o.getOverridesForUser(userID).QueryReadyIndexNumDays
}

// TSDBMaxQueryParallelism returns the limit to the number of sub-queries the
// frontend will process in parallel for TSDB schemas.
func (o *Overrides) TSDBMaxQueryParallelism(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).TSDBMaxQueryParallelism
}

// TSDBMaxBytesPerShard returns the maximum number of bytes assigned to a specific shard in a tsdb query
func (o *Overrides) TSDBMaxBytesPerShard(userID string) int {
	return o.getOverridesForUser(userID).TSDBMaxBytesPerShard.Val()
}

// TSDBShardingStrategy returns the sharding strategy to use in query planning.
func (o *Overrides) TSDBShardingStrategy(userID string) string {
	return o.getOverridesForUser(userID).TSDBShardingStrategy
}

func (o *Overrides) TSDBPrecomputeChunks(userID string) bool {
	return o.getOverridesForUser(userID).TSDBPrecomputeChunks
}

// MaxQueryParallelism returns the limit to the number of sub-queries the
// frontend will process in parallel.
func (o *Overrides) MaxQueryParallelism(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).MaxQueryParallelism
}

// CardinalityLimit whether to enforce the presence of a metric name.
func (o *Overrides) CardinalityLimit(userID string) int {
	return o.getOverridesForUser(userID).CardinalityLimit
}

// MaxStreamsMatchersPerQuery returns the limit to number of streams matchers per query.
func (o *Overrides) MaxStreamsMatchersPerQuery(_ context.Context, userID string) int {
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

// InstantMetricQuerySplitDuration returns the tenant specific instant metric queries splitby interval applied in the query frontend.
func (o *Overrides) InstantMetricQuerySplitDuration(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).InstantMetricQuerySplitDuration)
}

// MetadataQuerySplitDuration returns the tenant specific metadata splitby interval applied in the query frontend.
func (o *Overrides) MetadataQuerySplitDuration(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MetadataQuerySplitDuration)
}

// RecentMetadataQuerySplitDuration returns the tenant specific splitby interval for recent metadata queries.
func (o *Overrides) RecentMetadataQuerySplitDuration(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).RecentMetadataQuerySplitDuration)
}

// RecentMetadataQueryWindow returns the tenant specific time window used to determine recent metadata queries.
func (o *Overrides) RecentMetadataQueryWindow(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).RecentMetadataQueryWindow)
}

// IngesterQuerySplitDuration returns the tenant specific splitby interval applied in the query frontend when querying
// during the `query_ingesters_within` window.
func (o *Overrides) IngesterQuerySplitDuration(userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).IngesterQuerySplitDuration)
}

// MaxQueryBytesRead returns the maximum bytes a query can read.
func (o *Overrides) MaxQueryBytesRead(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).MaxQueryBytesRead.Val()
}

// MaxQuerierBytesRead returns the maximum bytes a sub query can read after splitting and sharding.
func (o *Overrides) MaxQuerierBytesRead(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).MaxQuerierBytesRead.Val()
}

// MaxConcurrentTailRequests returns the limit to number of concurrent tail requests.
func (o *Overrides) MaxConcurrentTailRequests(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).MaxConcurrentTailRequests
}

// MaxLineSize returns the maximum size in bytes the distributor should allow.
func (o *Overrides) MaxLineSize(userID string) int {
	return o.getOverridesForUser(userID).MaxLineSize.Val()
}

// MaxLineSizeTruncate returns whether lines longer than max should be truncated.
func (o *Overrides) MaxLineSizeTruncate(userID string) bool {
	return o.getOverridesForUser(userID).MaxLineSizeTruncate
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (o *Overrides) MaxEntriesLimitPerQuery(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).MaxEntriesLimitPerQuery
}

func (o *Overrides) QueryTimeout(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).QueryTimeout)
}

func (o *Overrides) MaxCacheFreshness(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxCacheFreshness)
}

func (o *Overrides) MaxMetadataCacheFreshness(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxMetadataCacheFreshness)
}

func (o *Overrides) MaxStatsCacheFreshness(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxStatsCacheFreshness)
}

// MaxQueryLookback returns the max lookback period of queries.
func (o *Overrides) MaxQueryLookback(_ context.Context, userID string) time.Duration {
	return time.Duration(o.getOverridesForUser(userID).MaxQueryLookback)
}

// RulerTenantShardSize returns shard size (number of rulers) used by this tenant when using shuffle-sharding strategy.
func (o *Overrides) RulerTenantShardSize(userID string) int {
	return o.getOverridesForUser(userID).RulerTenantShardSize
}

func (o *Overrides) IngestionPartitionsTenantShardSize(userID string) int {
	return o.getOverridesForUser(userID).IngestionPartitionsTenantShardSize
}

// RulerMaxRulesPerRuleGroup returns the maximum number of rules per rule group for a given user.
func (o *Overrides) RulerMaxRulesPerRuleGroup(userID string) int {
	return o.getOverridesForUser(userID).RulerMaxRulesPerRuleGroup
}

// RulerMaxRuleGroupsPerTenant returns the maximum number of rule groups for a given user.
func (o *Overrides) RulerMaxRuleGroupsPerTenant(userID string) int {
	return o.getOverridesForUser(userID).RulerMaxRuleGroupsPerTenant
}

// RulerAlertManagerConfig returns the alertmanager configurations to use for a given user.
func (o *Overrides) RulerAlertManagerConfig(userID string) *ruler_config.AlertManagerConfig {
	return o.getOverridesForUser(userID).RulerAlertManagerConfig
}

// RulerRemoteWriteDisabled returns whether remote-write is disabled for a given user or not.
func (o *Overrides) RulerRemoteWriteDisabled(userID string) bool {
	return o.getOverridesForUser(userID).RulerRemoteWriteDisabled
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteURL returns the remote-write URL to use for a given user.
func (o *Overrides) RulerRemoteWriteURL(userID string) string {
	return o.getOverridesForUser(userID).RulerRemoteWriteURL
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteTimeout returns the duration after which to timeout a remote-write request for a given user.
func (o *Overrides) RulerRemoteWriteTimeout(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteTimeout
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteHeaders returns the headers to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteHeaders(userID string) map[string]string {
	return o.getOverridesForUser(userID).RulerRemoteWriteHeaders.Map()
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteRelabelConfigs returns the write relabel configs to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteRelabelConfigs(userID string) []*util.RelabelConfig {
	return o.getOverridesForUser(userID).RulerRemoteWriteRelabelConfigs
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueCapacity returns the queue capacity to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueCapacity(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueCapacity
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueMinShards returns the minimum shards to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueMinShards(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMinShards
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueMaxShards returns the maximum shards to use in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueMaxShards(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMaxShards
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueMaxSamplesPerSend returns the max samples to send in a remote-write for a given user.
func (o *Overrides) RulerRemoteWriteQueueMaxSamplesPerSend(userID string) int {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMaxSamplesPerSend
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueBatchSendDeadline returns the maximum time a sample will be buffered before being discarded for a given user.
func (o *Overrides) RulerRemoteWriteQueueBatchSendDeadline(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueBatchSendDeadline
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueMinBackoff returns the minimum time for an exponential backoff for a given user.
func (o *Overrides) RulerRemoteWriteQueueMinBackoff(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMinBackoff
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueMaxBackoff returns the maximum time for an exponential backoff for a given user.
func (o *Overrides) RulerRemoteWriteQueueMaxBackoff(userID string) time.Duration {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueMaxBackoff
}

// Deprecated: use RulerRemoteWriteConfig instead
// RulerRemoteWriteQueueRetryOnRateLimit returns whether to retry failed remote-write requests (429 response) for a given user.
func (o *Overrides) RulerRemoteWriteQueueRetryOnRateLimit(userID string) bool {
	return o.getOverridesForUser(userID).RulerRemoteWriteQueueRetryOnRateLimit
}

// Deprecated: use RulerRemoteWriteConfig instead
func (o *Overrides) RulerRemoteWriteSigV4Config(userID string) *sigv4.SigV4Config {
	return o.getOverridesForUser(userID).RulerRemoteWriteSigV4Config
}

// RulerRemoteWriteConfig returns the remote-write configurations to use for a given user and a given remote client.
func (o *Overrides) RulerRemoteWriteConfig(userID string, id string) *config.RemoteWriteConfig {
	if c, ok := o.getOverridesForUser(userID).RulerRemoteWriteConfig[id]; ok {
		return &c
	}

	return nil
}

// RulerRemoteEvaluationTimeout returns the duration after which to timeout a remote rule evaluation request for a given user.
func (o *Overrides) RulerRemoteEvaluationTimeout(userID string) time.Duration {
	// if not defined, use the base query timeout
	timeout := o.getOverridesForUser(userID).RulerRemoteEvaluationTimeout
	if timeout <= 0 {
		return time.Duration(o.getOverridesForUser(userID).QueryTimeout)
	}

	return timeout
}

// RulerRemoteEvaluationMaxResponseSize returns the maximum allowable response size from a remote rule evaluation for a given user.
func (o *Overrides) RulerRemoteEvaluationMaxResponseSize(userID string) int64 {
	return o.getOverridesForUser(userID).RulerRemoteEvaluationMaxResponseSize
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

func (o *Overrides) DeletionMode(userID string) string {
	return o.getOverridesForUser(userID).DeletionMode
}

func (o *Overrides) ShardStreams(userID string) shardstreams.Config {
	return o.getOverridesForUser(userID).ShardStreams
}

func (o *Overrides) BlockedQueries(_ context.Context, userID string) []*validation.BlockedQuery {
	return o.getOverridesForUser(userID).BlockedQueries
}

func (o *Overrides) RequiredLabels(_ context.Context, userID string) []string {
	return o.getOverridesForUser(userID).RequiredLabels
}

func (o *Overrides) RequiredNumberLabels(_ context.Context, userID string) int {
	return o.getOverridesForUser(userID).RequiredNumberLabels
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

func (o *Overrides) IncrementDuplicateTimestamps(userID string) bool {
	return o.getOverridesForUser(userID).IncrementDuplicateTimestamp
}

func (o *Overrides) DiscoverGenericFields(userID string) map[string][]string {
	return o.getOverridesForUser(userID).DiscoverGenericFields.Fields
}

func (o *Overrides) DiscoverServiceName(userID string) []string {
	return o.getOverridesForUser(userID).DiscoverServiceName
}

func (o *Overrides) DiscoverLogLevels(userID string) bool {
	return o.getOverridesForUser(userID).DiscoverLogLevels
}

func (o *Overrides) LogLevelFields(userID string) []string {
	return o.getOverridesForUser(userID).LogLevelFields
}

func (o *Overrides) LogLevelFromJSONMaxDepth(userID string) int {
	return o.getOverridesForUser(userID).LogLevelFromJSONMaxDepth
}

// VolumeEnabled returns whether volume endpoints are enabled for a user.
func (o *Overrides) VolumeEnabled(userID string) bool {
	return o.getOverridesForUser(userID).VolumeEnabled
}

func (o *Overrides) VolumeMaxSeries(userID string) int {
	return o.getOverridesForUser(userID).VolumeMaxSeries
}

func (o *Overrides) IndexGatewayShardSize(userID string) int {
	return o.getOverridesForUser(userID).IndexGatewayShardSize
}

func (o *Overrides) BloomGatewayEnabled(userID string) bool {
	return o.getOverridesForUser(userID).BloomGatewayEnabled
}

func (o *Overrides) BloomCreationEnabled(userID string) bool {
	return o.getOverridesForUser(userID).BloomCreationEnabled
}

func (o *Overrides) BloomPlanningStrategy(userID string) string {
	return o.getOverridesForUser(userID).BloomPlanningStrategy
}

func (o *Overrides) BloomSplitSeriesKeyspaceBy(userID string) int {
	return o.getOverridesForUser(userID).BloomSplitSeriesKeyspaceBy
}

func (o *Overrides) BloomTaskTargetSeriesChunksSizeBytes(userID string) uint64 {
	return uint64(o.getOverridesForUser(userID).BloomTaskTargetSeriesChunkSize)
}

func (o *Overrides) BloomBuildMaxBuilders(userID string) int {
	return o.getOverridesForUser(userID).BloomBuildMaxBuilders
}

func (o *Overrides) BuilderResponseTimeout(userID string) time.Duration {
	return o.getOverridesForUser(userID).BloomBuilderResponseTimeout
}

func (o *Overrides) PrefetchBloomBlocks(userID string) bool {
	return o.getOverridesForUser(userID).BloomPrefetchBlocks
}

func (o *Overrides) BloomTaskMaxRetries(userID string) int {
	return o.getOverridesForUser(userID).BloomBuildTaskMaxRetries
}

func (o *Overrides) BloomMaxBlockSize(userID string) int {
	return o.getOverridesForUser(userID).BloomMaxBlockSize.Val()
}

func (o *Overrides) BloomMaxBloomSize(userID string) int {
	return o.getOverridesForUser(userID).BloomMaxBloomSize.Val()
}

func (o *Overrides) BloomBlockEncoding(userID string) string {
	return o.getOverridesForUser(userID).BloomBlockEncoding
}

func (o *Overrides) AllowStructuredMetadata(userID string) bool {
	return o.getOverridesForUser(userID).AllowStructuredMetadata
}

func (o *Overrides) MaxStructuredMetadataSize(userID string) int {
	return o.getOverridesForUser(userID).MaxStructuredMetadataSize.Val()
}

func (o *Overrides) MaxStructuredMetadataCount(userID string) int {
	return o.getOverridesForUser(userID).MaxStructuredMetadataEntriesCount
}

func (o *Overrides) OTLPConfig(userID string) push.OTLPConfig {
	return o.getOverridesForUser(userID).OTLPConfig
}

func (o *Overrides) BlockIngestionUntil(userID string) time.Time {
	return time.Time(o.getOverridesForUser(userID).BlockIngestionUntil)
}

func (o *Overrides) BlockIngestionStatusCode(userID string) int {
	return o.getOverridesForUser(userID).BlockIngestionStatusCode
}

// BlockIngestionPolicyUntil returns the time until the ingestion policy is blocked for a given user.
// Order of priority is: named policy block > global policy block. The global policy block is enforced
// only if the policy is empty.
func (o *Overrides) BlockIngestionPolicyUntil(userID string, policy string) time.Time {
	limits := o.getOverridesForUser(userID)

	if forPolicy, ok := limits.BlockIngestionPolicyUntil[policy]; ok {
		return time.Time(forPolicy)
	}

	// We enforce the global policy on streams not matching any policy
	if policy == "" {
		if forPolicy, ok := limits.BlockIngestionPolicyUntil[GlobalPolicy]; ok {
			return time.Time(forPolicy)
		}
	}

	return time.Time{} // Zero time means no blocking
}

func (o *Overrides) EnforcedLabels(userID string) []string {
	return o.getOverridesForUser(userID).EnforcedLabels
}

// PolicyEnforcedLabels returns the labels enforced by the policy for a given user.
// The output is the union of the global and policy specific labels.
func (o *Overrides) PolicyEnforcedLabels(userID string, policy string) []string {
	limits := o.getOverridesForUser(userID)
	return append(limits.PolicyEnforcedLabels[GlobalPolicy], limits.PolicyEnforcedLabels[policy]...)
}

func (o *Overrides) PoliciesStreamMapping(userID string) PolicyStreamMapping {
	return o.getOverridesForUser(userID).PolicyStreamMapping
}

func (o *Overrides) ShardAggregations(userID string) []string {
	return o.getOverridesForUser(userID).ShardAggregations
}

func (o *Overrides) PatternIngesterTokenizableJSONFields(userID string) []string {
	defaultFields := o.getOverridesForUser(userID).PatternIngesterTokenizableJSONFieldsDefault
	appendFields := o.getOverridesForUser(userID).PatternIngesterTokenizableJSONFieldsAppend
	deleteFields := o.getOverridesForUser(userID).PatternIngesterTokenizableJSONFieldsDelete

	outputMap := make(map[string]struct{}, len(defaultFields)+len(appendFields))

	for _, field := range defaultFields {
		outputMap[field] = struct{}{}
	}

	for _, field := range appendFields {
		outputMap[field] = struct{}{}
	}

	for _, field := range deleteFields {
		delete(outputMap, field)
	}

	output := make([]string, 0, len(outputMap))
	for field := range outputMap {
		output = append(output, field)
	}

	return output
}

func (o *Overrides) PatternIngesterTokenizableJSONFieldsAppend(userID string) []string {
	return o.getOverridesForUser(userID).PatternIngesterTokenizableJSONFieldsAppend
}

func (o *Overrides) PatternIngesterTokenizableJSONFieldsDelete(userID string) []string {
	return o.getOverridesForUser(userID).PatternIngesterTokenizableJSONFieldsDelete
}

func (o *Overrides) MetricAggregationEnabled(userID string) bool {
	return o.getOverridesForUser(userID).MetricAggregationEnabled
}

func (o *Overrides) EnableMultiVariantQueries(userID string) bool {
	return o.getOverridesForUser(userID).EnableMultiVariantQueries
}

// S3SSEType returns the per-tenant S3 SSE type.
func (o *Overrides) S3SSEType(user string) string {
	return o.getOverridesForUser(user).S3SSEType
}

// S3SSEKMSKeyID returns the per-tenant S3 KMS-SSE key id.
func (o *Overrides) S3SSEKMSKeyID(user string) string {
	return o.getOverridesForUser(user).S3SSEKMSKeyID
}

// S3SSEKMSEncryptionContext returns the per-tenant S3 KMS-SSE encryption context.
func (o *Overrides) S3SSEKMSEncryptionContext(user string) string {
	return o.getOverridesForUser(user).S3SSEKMSEncryptionContext
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

func (o *Overrides) SimulatedPushLatency(userID string) time.Duration {
	return o.getOverridesForUser(userID).SimulatedPushLatency
}
