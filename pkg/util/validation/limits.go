package validation

import (
	"flag"
	"os"
	"time"

	"github.com/cortexproject/cortex/pkg/util/validation"

	"gopkg.in/yaml.v2"
)

// Limits describe all the limits for users; can be used to describe global default
// limits via flags, or per-user limits via yaml config.
type Limits struct {
	// Distributor enforced limits.
	IngestionRate          float64       `yaml:"ingestion_rate_mb"`
	IngestionBurstSize     float64       `yaml:"ingestion_burst_size_mb"`
	MaxLabelNameLength     int           `yaml:"max_label_name_length"`
	MaxLabelValueLength    int           `yaml:"max_label_value_length"`
	MaxLabelNamesPerSeries int           `yaml:"max_label_names_per_series"`
	RejectOldSamples       bool          `yaml:"reject_old_samples"`
	RejectOldSamplesMaxAge time.Duration `yaml:"reject_old_samples_max_age"`
	CreationGracePeriod    time.Duration `yaml:"creation_grace_period"`
	EnforceMetricName      bool          `yaml:"enforce_metric_name"`

	// Ingester enforced limits.
	MaxStreamsPerUser int `yaml:"max_streams_per_user"`

	// Querier enforced limits.
	MaxChunksPerQuery          int           `yaml:"max_chunks_per_query"`
	MaxQueryLength             time.Duration `yaml:"max_query_length"`
	MaxQueryParallelism        int           `yaml:"max_query_parallelism"`
	CardinalityLimit           int           `yaml:"cardinality_limit"`
	MaxStreamsMatchersPerQuery int           `yaml:"max_streams_matchers_per_query"`

	// Config for overrides, convenient if it goes here.
	PerTenantOverrideConfig string        `yaml:"per_tenant_override_config"`
	PerTenantOverridePeriod time.Duration `yaml:"per_tenant_override_period"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (l *Limits) RegisterFlags(f *flag.FlagSet) {
	f.Float64Var(&l.IngestionRate, "distributor.ingestion-rate-limit-mb", 4, "Per-user ingestion rate limit in sample size per second. Units in MB.")
	f.Float64Var(&l.IngestionBurstSize, "distributor.ingestion-burst-size-mb", 6, "Per-user allowed ingestion burst size (in sample size). Units in MB. Warning, very high limits will be reset every -distributor.limiter-reload-period.")
	f.IntVar(&l.MaxLabelNameLength, "validation.max-length-label-name", 1024, "Maximum length accepted for label names")
	f.IntVar(&l.MaxLabelValueLength, "validation.max-length-label-value", 2048, "Maximum length accepted for label value. This setting also applies to the metric name")
	f.IntVar(&l.MaxLabelNamesPerSeries, "validation.max-label-names-per-series", 30, "Maximum number of label names per series.")
	f.BoolVar(&l.RejectOldSamples, "validation.reject-old-samples", false, "Reject old samples.")
	f.DurationVar(&l.RejectOldSamplesMaxAge, "validation.reject-old-samples.max-age", 14*24*time.Hour, "Maximum accepted sample age before rejecting.")
	f.DurationVar(&l.CreationGracePeriod, "validation.create-grace-period", 10*time.Minute, "Duration which table will be created/deleted before/after it's needed; we won't accept sample from before this time.")
	f.BoolVar(&l.EnforceMetricName, "validation.enforce-metric-name", true, "Enforce every sample has a metric name.")

	f.IntVar(&l.MaxStreamsPerUser, "ingester.max-streams-per-user", 10e3, "Maximum number of active streams per user.")

	f.IntVar(&l.MaxChunksPerQuery, "store.query-chunk-limit", 2e6, "Maximum number of chunks that can be fetched in a single query.")
	f.DurationVar(&l.MaxQueryLength, "store.max-query-length", 0, "Limit to length of chunk store queries, 0 to disable.")
	f.IntVar(&l.MaxQueryParallelism, "querier.max-query-parallelism", 14, "Maximum number of queries will be scheduled in parallel by the frontend.")
	f.IntVar(&l.CardinalityLimit, "store.cardinality-limit", 1e5, "Cardinality limit for index queries.")
	f.IntVar(&l.MaxStreamsMatchersPerQuery, "querier.max-streams-matcher-per-query", 1000, "Limit the number of streams matchers per query")

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

// Overrides periodically fetch a set of per-user overrides, and provides convenience
// functions for fetching the correct value.
type Overrides struct {
	overridesManager *validation.OverridesManager
}

// NewOverrides makes a new Overrides.
// We store the supplied limits in a global variable to ensure per-tenant limits
// are defaulted to those values.  As such, the last call to NewOverrides will
// become the new global defaults.
func NewOverrides(defaults Limits) (*Overrides, error) {
	defaultLimits = &defaults
	overridesManagerConfig := validation.OverridesManagerConfig{
		OverridesReloadPeriod: defaults.PerTenantOverridePeriod,
		OverridesLoadPath:     defaults.PerTenantOverrideConfig,
		OverridesLoader:       loadOverrides,
		Defaults:              &defaults,
	}

	overridesManager, err := validation.NewOverridesManager(overridesManagerConfig)
	if err != nil {
		return nil, err
	}

	return &Overrides{
		overridesManager: overridesManager,
	}, nil
}

// Stop background reloading of overrides.
func (o *Overrides) Stop() {
	o.overridesManager.Stop()
}

// IngestionRate returns the limit on ingester rate (samples per second).
func (o *Overrides) IngestionRate(userID string) float64 {
	return o.overridesManager.GetLimits(userID).(*Limits).IngestionRate
}

// IngestionBurstSize returns the burst size for ingestion rate.
func (o *Overrides) IngestionBurstSize(userID string) float64 {
	return o.overridesManager.GetLimits(userID).(*Limits).IngestionBurstSize
}

// MaxLabelNameLength returns maximum length a label name can be.
func (o *Overrides) MaxLabelNameLength(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxLabelNameLength
}

// MaxLabelValueLength returns maximum length a label value can be. This also is
// the maximum length of a metric name.
func (o *Overrides) MaxLabelValueLength(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxLabelValueLength
}

// MaxLabelNamesPerSeries returns maximum number of label/value pairs timeseries.
func (o *Overrides) MaxLabelNamesPerSeries(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxLabelNamesPerSeries
}

// RejectOldSamples returns true when we should reject samples older than certain
// age.
func (o *Overrides) RejectOldSamples(userID string) bool {
	return o.overridesManager.GetLimits(userID).(*Limits).RejectOldSamples
}

// RejectOldSamplesMaxAge returns the age at which samples should be rejected.
func (o *Overrides) RejectOldSamplesMaxAge(userID string) time.Duration {
	return o.overridesManager.GetLimits(userID).(*Limits).RejectOldSamplesMaxAge
}

// CreationGracePeriod is misnamed, and actually returns how far into the future
// we should accept samples.
func (o *Overrides) CreationGracePeriod(userID string) time.Duration {
	return o.overridesManager.GetLimits(userID).(*Limits).CreationGracePeriod
}

// MaxStreamsPerUser returns the maximum number of streams a user is allowed to store.
func (o *Overrides) MaxStreamsPerUser(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxStreamsPerUser
}

// MaxChunksPerQuery returns the maximum number of chunks allowed per query.
func (o *Overrides) MaxChunksPerQuery(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxChunksPerQuery
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (o *Overrides) MaxQueryLength(userID string) time.Duration {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxQueryLength
}

// MaxQueryParallelism returns the limit to the number of sub-queries the
// frontend will process in parallel.
func (o *Overrides) MaxQueryParallelism(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxQueryParallelism
}

// EnforceMetricName whether to enforce the presence of a metric name.
func (o *Overrides) EnforceMetricName(userID string) bool {
	return o.overridesManager.GetLimits(userID).(*Limits).EnforceMetricName
}

// CardinalityLimit whether to enforce the presence of a metric name.
func (o *Overrides) CardinalityLimit(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).CardinalityLimit
}

// MaxStreamsMatchersPerQuery returns the limit to number of streams matchers per query.
func (o *Overrides) MaxStreamsMatchersPerQuery(userID string) int {
	return o.overridesManager.GetLimits(userID).(*Limits).MaxStreamsMatchersPerQuery
}

// Loads overrides and returns the limits as an interface to store them in OverridesManager.
// We need to implement it here since OverridesManager must store type Limits in an interface but
// it doesn't know its definition to initialize it.
// We could have used yamlv3.Node for this but there is no way to enforce strict decoding due to a bug in it
// TODO: Use yamlv3.Node to move this to OverridesManager after https://github.com/go-yaml/yaml/issues/460 is fixed
func loadOverrides(filename string) (map[string]interface{}, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var overrides struct {
		Overrides map[string]*Limits `yaml:"overrides"`
	}

	decoder := yaml.NewDecoder(f)
	decoder.SetStrict(true)
	if err := decoder.Decode(&overrides); err != nil {
		return nil, err
	}

	overridesAsInterface := map[string]interface{}{}
	for userID := range overrides.Overrides {
		overridesAsInterface[userID] = overrides.Overrides[userID]
	}

	return overridesAsInterface, nil
}
