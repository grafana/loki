// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/cortex/runtime_config_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package mimir

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/mimir/pkg/util/validation"
)

// Given limits are usually loaded via a config file, and that
// a configmap is limited to 1MB, we need to minimise the limits file.
// One way to do it is via YAML anchors.
func TestLoadRuntimeConfig_ShouldLoadAnchoredYAML(t *testing.T) {
	validation.SetDefaultLimitsForYAMLUnmarshalling(validation.Limits{})

	yamlFile := strings.NewReader(`
overrides:
  '1234': &id001
    ingestion_burst_size: 15000
    ingestion_rate: 1500
    max_global_series_per_metric: 7000
    max_global_series_per_user: 15000
    ruler_max_rule_groups_per_tenant: 20
    ruler_max_rules_per_rule_group: 20
  '1235': *id001
  '1236': *id001
`)
	runtimeCfg, err := loadRuntimeConfig(yamlFile)
	require.NoError(t, err)

	limits := validation.Limits{
		IngestionRate:                       1500,
		IngestionBurstSize:                  15000,
		MaxGlobalSeriesPerUser:              15000,
		MaxGlobalSeriesPerMetric:            7000,
		RulerMaxRulesPerRuleGroup:           20,
		RulerMaxRuleGroupsPerTenant:         20,
		NotificationRateLimitPerIntegration: validation.NotificationRateLimitMap{},
	}

	loadedLimits := runtimeCfg.(*runtimeConfigValues).TenantLimits
	require.Equal(t, 3, len(loadedLimits))
	require.Equal(t, limits, *loadedLimits["1234"])
	require.Equal(t, limits, *loadedLimits["1235"])
	require.Equal(t, limits, *loadedLimits["1236"])
}

func TestLoadRuntimeConfig_ShouldLoadEmptyFile(t *testing.T) {
	yamlFile := strings.NewReader(`
# This is an empty YAML.
`)
	actual, err := loadRuntimeConfig(yamlFile)
	require.NoError(t, err)
	assert.Equal(t, &runtimeConfigValues{}, actual)
}

func TestLoadRuntimeConfig_MissingPointerFieldsAreNil(t *testing.T) {
	yamlFile := strings.NewReader(`
# This is an empty YAML.
`)
	actual, err := loadRuntimeConfig(yamlFile)
	require.NoError(t, err)

	actualCfg, ok := actual.(*runtimeConfigValues)
	require.Truef(t, ok, "expected to be able to cast %+v to runtimeConfigValues", actual)

	// Ensure that when settings are omitted, the pointers are nil. See #4228
	assert.Nil(t, actualCfg.IngesterLimits)
}

func TestLoadRuntimeConfig_ShouldReturnErrorOnMultipleDocumentsInTheConfig(t *testing.T) {
	cases := []string{
		`
---
---
`, `
---
overrides:
  '1234':
    ingestion_burst_size: 123
---
overrides:
  '1234':
    ingestion_burst_size: 123
`, `
---
# This is an empty YAML.
---
overrides:
  '1234':
    ingestion_burst_size: 123
`, `
---
overrides:
  '1234':
    ingestion_burst_size: 123
---
# This is an empty YAML.
`,
	}

	for _, tc := range cases {
		actual, err := loadRuntimeConfig(strings.NewReader(tc))
		assert.Equal(t, errMultipleDocuments, err)
		assert.Nil(t, actual)
	}
}
