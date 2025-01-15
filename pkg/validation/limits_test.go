package validation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logql"
)

func TestLimitsTagsYamlMatchJson(t *testing.T) {
	limits := reflect.TypeOf(Limits{})
	n := limits.NumField()
	var mismatch []string

	for i := 0; i < n; i++ {
		field := limits.Field(i)

		// Note that we aren't requiring YAML and JSON tags to match, just that
		// they either both exist or both don't exist.
		hasYAMLTag := field.Tag.Get("yaml") != ""
		hasJSONTag := field.Tag.Get("json") != ""

		if hasYAMLTag != hasJSONTag {
			mismatch = append(mismatch, field.Name)
		}
	}

	assert.Empty(t, mismatch, "expected no mismatched JSON and YAML tags")
}

func TestLimitsYamlMatchJson(t *testing.T) {
	inputYAML := `
ingestion_rate_strategy: "some-strategy"
ingestion_rate_mb: 34
ingestion_burst_size_mb: 40
max_label_name_length: 10
max_label_value_length: 20
max_label_names_per_series: 30
reject_old_samples: true
reject_old_samples_max_age: 40s
creation_grace_period: 50s
enforce_metric_name: true
max_line_size: 60
max_line_size_truncate: true
max_streams_per_user: 70
max_global_streams_per_user: 80
max_chunks_per_query: 90
max_query_series: 100
max_query_lookback: 110s
max_query_length: 120s
max_query_parallelism: 130
cardinality_limit: 140
max_streams_matchers_per_query: 150
max_concurrent_tail_requests: 160
max_entries_limit_per_query: 170
max_cache_freshness_per_query: 180s
split_queries_by_interval: 190s
ruler_evaluation_delay_duration: 200s
ruler_max_rules_per_rule_group: 210
ruler_max_rule_groups_per_tenant: 220
ruler_remote_write_sigv4_config:
  region: us-east-1
per_tenant_override_config: ""
per_tenant_override_period: 230s
query_timeout: 5m
shard_streams:
  enabled: true
  desired_rate: 4mb
  logging_enabled: true
blocked_queries:
  - pattern: ".*foo.*"
    regex: true
volume_enabled: true
volume_max_series: 10001
`
	inputJSON := `
 {
  "ingestion_rate_strategy": "some-strategy",
  "ingestion_rate_mb": 34,
  "ingestion_burst_size_mb": 40,
  "max_label_name_length": 10,
  "max_label_value_length": 20,
  "max_label_names_per_series": 30,
  "reject_old_samples": true,
  "reject_old_samples_max_age": "40s",
  "creation_grace_period": "50s",
  "enforce_metric_name": true,
  "max_line_size": "60",
  "max_line_size_truncate": true,
  "max_streams_per_user": 70,
  "max_global_streams_per_user": 80,
  "max_chunks_per_query": 90,
  "max_query_series": 100,
  "max_query_lookback": "110s",
  "max_query_length": "120s",
  "max_query_parallelism": 130,
  "cardinality_limit": 140,
  "max_streams_matchers_per_query": 150,
  "max_concurrent_tail_requests": 160,
  "max_entries_limit_per_query": 170,
  "max_cache_freshness_per_query": "180s",
  "split_queries_by_interval": "190s",
  "ruler_evaluation_delay_duration": "200s",
  "ruler_max_rules_per_rule_group": 210,
  "ruler_max_rule_groups_per_tenant":220,
  "ruler_remote_write_sigv4_config": {
    "region": "us-east-1"
  },
  "per_tenant_override_config": "",
  "per_tenant_override_period": "230s",
  "query_timeout": "5m",
  "shard_streams": {
    "desired_rate": "4mb",
    "enabled": true,
    "logging_enabled": true
  },
  "blocked_queries": [
	{
		"pattern": ".*foo.*",
		"regex": true
	}
  ],
  "volume_enabled": true,
  "volume_max_series": 10001
 }
`

	limitsYAML := Limits{}
	err := yaml.Unmarshal([]byte(inputYAML), &limitsYAML)
	require.NoError(t, err, "expected to be able to unmarshal from YAML")

	limitsJSON := Limits{}
	err = json.Unmarshal([]byte(inputJSON), &limitsJSON)
	require.NoError(t, err, "expected to be able to unmarshal from JSON")

	assert.Equal(t, limitsYAML, limitsJSON)
}

func TestOverwriteMarshalingStringMapJSON(t *testing.T) {
	m := NewOverwriteMarshalingStringMap(map[string]string{"foo": "bar"})

	require.Nil(t, json.Unmarshal([]byte(`{"bazz": "buzz"}`), &m))
	require.Equal(t, map[string]string{"bazz": "buzz"}, m.Map())
	out, err := json.Marshal(m)
	require.Nil(t, err)
	var back OverwriteMarshalingStringMap
	require.Nil(t, json.Unmarshal(out, &back))
	require.Equal(t, m, back)
}

func TestOverwriteMarshalingStringMapYAML(t *testing.T) {
	m := NewOverwriteMarshalingStringMap(map[string]string{"foo": "bar"})

	require.Nil(t, yaml.Unmarshal([]byte(`{"bazz": "buzz"}`), &m))
	require.Equal(t, map[string]string{"bazz": "buzz"}, m.Map())
	out, err := yaml.Marshal(m)
	require.Nil(t, err)
	var back OverwriteMarshalingStringMap
	require.Nil(t, yaml.Unmarshal(out, &back))
	require.Equal(t, m, back)
}

func TestLimitsDoesNotMutate(t *testing.T) {
	initialDefault := defaultLimits
	defer func() {
		defaultLimits = initialDefault
	}()

	defaultOTLPConfig := push.OTLPConfig{
		ResourceAttributes: push.ResourceAttributesConfig{
			IgnoreDefaults: true,
			AttributesConfig: []push.AttributesConfig{
				{
					Action:     push.IndexLabel,
					Attributes: []string{"pod"},
				},
			},
		},
	}

	// Set new defaults with non-nil values for non-scalar types
	newDefaults := Limits{
		RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
		StreamRetention: []StreamRetention{
			{
				Period:   model.Duration(24 * time.Hour),
				Selector: `{a="b"}`,
			},
		},
		OTLPConfig: defaultOTLPConfig,
	}
	SetDefaultLimitsForYAMLUnmarshalling(newDefaults)

	for _, tc := range []struct {
		desc string
		yaml string
		exp  Limits
	}{
		{
			desc: "map",
			yaml: `
ruler_remote_write_headers:
  foo: "bar"
`,
			exp: Limits{
				DiscoverGenericFields:   FieldDetectorConfig{},
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"foo": "bar"}},
				DiscoverServiceName:     []string{},
				LogLevelFields:          []string{},

				// Rest from new defaults
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
				OTLPConfig:     defaultOTLPConfig,
				EnforcedLabels: []string{},
			},
		},
		{
			desc: "empty map overrides defaults",
			yaml: `
ruler_remote_write_headers:
`,
			exp: Limits{
				DiscoverGenericFields: FieldDetectorConfig{},
				DiscoverServiceName:   []string{},
				LogLevelFields:        []string{},
				// Rest from new defaults
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
				OTLPConfig:     defaultOTLPConfig,
				EnforcedLabels: []string{},
			},
		},
		{
			desc: "slice",
			yaml: `
retention_stream:
  - period: '24h'
    selector: '{foo="bar"}'
`,
			exp: Limits{
				DiscoverGenericFields: FieldDetectorConfig{},
				DiscoverServiceName:   []string{},
				LogLevelFields:        []string{},
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{foo="bar"}`,
					},
				},

				// Rest from new defaults
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
				OTLPConfig:              defaultOTLPConfig,
				EnforcedLabels:          []string{},
			},
		},
		{
			desc: "scalar field",
			yaml: `
reject_old_samples: true
`,
			exp: Limits{
				RejectOldSamples:      true,
				DiscoverGenericFields: FieldDetectorConfig{},
				DiscoverServiceName:   []string{},
				LogLevelFields:        []string{},

				// Rest from new defaults
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
				OTLPConfig:     defaultOTLPConfig,
				EnforcedLabels: []string{},
			},
		},
		{
			desc: "per tenant query timeout",
			yaml: `
query_timeout: 5m
`,
			exp: Limits{
				DiscoverGenericFields: FieldDetectorConfig{},
				DiscoverServiceName:   []string{},
				LogLevelFields:        []string{},

				QueryTimeout: model.Duration(5 * time.Minute),

				// Rest from new defaults.
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
				OTLPConfig:     defaultOTLPConfig,
				EnforcedLabels: []string{},
			},
		},
	} {

		t.Run(tc.desc, func(t *testing.T) {
			var out Limits
			require.Nil(t, yaml.UnmarshalStrict([]byte(tc.yaml), &out))
			require.Equal(t, tc.exp, out)
		})
	}
}

func TestLimitsValidation(t *testing.T) {
	for _, tc := range []struct {
		limits   Limits
		expected error
	}{
		{
			limits:   Limits{DeletionMode: "disabled", BloomBlockEncoding: "none"},
			expected: nil,
		},
		{
			limits:   Limits{DeletionMode: "filter-only", BloomBlockEncoding: "none"},
			expected: nil,
		},
		{
			limits:   Limits{DeletionMode: "filter-and-delete", BloomBlockEncoding: "none"},
			expected: nil,
		},
		{
			limits:   Limits{DeletionMode: "something-else", BloomBlockEncoding: "none"},
			expected: deletionmode.ErrUnknownMode,
		},
		{
			limits:   Limits{DeletionMode: "disabled", BloomBlockEncoding: "unknown"},
			expected: fmt.Errorf("invalid encoding: unknown, supported: %s", compression.SupportedCodecs()),
		},
	} {
		desc := fmt.Sprintf("%s/%s", tc.limits.DeletionMode, tc.limits.BloomBlockEncoding)
		t.Run(desc, func(t *testing.T) {
			tc.limits.TSDBShardingStrategy = logql.PowerOfTwoVersion.String() // hacky but needed for test
			tc.limits.TSDBMaxBytesPerShard = DefaultTSDBMaxBytesPerShard
			if tc.expected == nil {
				require.NoError(t, tc.limits.Validate())
			} else {
				require.ErrorContains(t, tc.limits.Validate(), tc.expected.Error())
			}
		})
	}
}

func Test_PatternIngesterTokenizableJSONFields(t *testing.T) {
	for _, tc := range []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "only defaults",
			yaml: `
pattern_ingester_tokenizable_json_fields_default: log,message
`,
			expected: []string{"log", "message"},
		},
		{
			name: "with append",
			yaml: `
pattern_ingester_tokenizable_json_fields_default: log,message
pattern_ingester_tokenizable_json_fields_append: msg,body
`,
			expected: []string{"log", "message", "msg", "body"},
		},
		{
			name: "with delete",
			yaml: `
pattern_ingester_tokenizable_json_fields_default: log,message
pattern_ingester_tokenizable_json_fields_delete: message
`,
			expected: []string{"log"},
		},
		{
			name: "with append and delete from default",
			yaml: `
pattern_ingester_tokenizable_json_fields_default: log,message
pattern_ingester_tokenizable_json_fields_append: msg,body
pattern_ingester_tokenizable_json_fields_delete: message
`,
			expected: []string{"log", "msg", "body"},
		},
		{
			name: "with append and delete from append",
			yaml: `
pattern_ingester_tokenizable_json_fields_default: log,message
pattern_ingester_tokenizable_json_fields_append: msg,body
pattern_ingester_tokenizable_json_fields_delete: body
`,
			expected: []string{"log", "message", "msg"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overrides := Overrides{
				defaultLimits: &Limits{},
			}
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), overrides.defaultLimits))

			actual := overrides.PatternIngesterTokenizableJSONFields("fake")
			require.ElementsMatch(t, tc.expected, actual)
		})
	}
}

func Test_MetricAggregationEnabled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		yaml     string
		expected bool
	}{
		{
			name: "when true",
			yaml: `
metric_aggregation_enabled: true
`,
			expected: true,
		},
		{
			name: "when false",
			yaml: `
metric_aggregation_enabled: false
`,
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overrides := Overrides{
				defaultLimits: &Limits{},
			}
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), overrides.defaultLimits))

			actual := overrides.MetricAggregationEnabled("fake")
			require.Equal(t, tc.expected, actual)
		})
	}
}
