package validation

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/deletionmode"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
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
  ]
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

	// Set new defaults with non-nil values for non-scalar types
	newDefaults := Limits{
		RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
		StreamRetention: []StreamRetention{
			{
				Period:   model.Duration(24 * time.Hour),
				Selector: `{a="b"}`,
			},
		},
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
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"foo": "bar"}},

				// Rest from new defaults
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
			},
		},
		{
			desc: "empty map overrides defaults",
			yaml: `
ruler_remote_write_headers:
`,
			exp: Limits{

				// Rest from new defaults
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
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
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{foo="bar"}`,
					},
				},

				// Rest from new defaults
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
			},
		},
		{
			desc: "scalar field",
			yaml: `
reject_old_samples: true
`,
			exp: Limits{
				RejectOldSamples: true,

				// Rest from new defaults
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
			},
		},
		{
			desc: "per tenant query timeout",
			yaml: `
query_timeout: 5m
`,
			exp: Limits{
				QueryTimeout: model.Duration(5 * time.Minute),

				// Rest from new defaults.
				RulerRemoteWriteHeaders: OverwriteMarshalingStringMap{map[string]string{"a": "b"}},
				StreamRetention: []StreamRetention{
					{
						Period:   model.Duration(24 * time.Hour),
						Selector: `{a="b"}`,
					},
				},
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
		mode     string
		expected error
	}{
		{mode: "disabled", expected: nil},
		{mode: "filter-only", expected: nil},
		{mode: "filter-and-delete", expected: nil},
		{mode: "something-else", expected: deletionmode.ErrUnknownMode},
	} {
		limits := Limits{DeletionMode: tc.mode}
		require.True(t, errors.Is(limits.Validate(), tc.expected))
	}
}
