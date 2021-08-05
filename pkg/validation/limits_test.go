package validation

import (
	"encoding/json"
	"reflect"
	"testing"

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
per_tenant_override_config: ""
per_tenant_override_period: 230s
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
  "max_line_size": 60,
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
  "per_tenant_override_config": "",
  "per_tenant_override_period": "230s"
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
