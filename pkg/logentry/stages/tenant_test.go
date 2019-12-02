package stages

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/grafana/loki/pkg/promtail/client"
	lokiutil "github.com/grafana/loki/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantStage_Validation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config      *TenantConfig
		expectedErr *string
	}{
		"should pass on source config option set": {
			config: &TenantConfig{
				Source: "tenant",
			},
			expectedErr: nil,
		},
		"should pass on value config option set": {
			config: &TenantConfig{
				Value: "team-a",
			},
			expectedErr: nil,
		},
		"should fail on missing source and value": {
			config:      &TenantConfig{},
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptySourceOrValue),
		},
		"should fail on empty source": {
			config: &TenantConfig{
				Source: "",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptySourceOrValue),
		},
		"should fail on empty value": {
			config: &TenantConfig{
				Value: "",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptySourceOrValue),
		},
		"should fail on both source and value set": {
			config: &TenantConfig{
				Source: "tenant",
				Value:  "team-a",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageConflictingSourceAndValue),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			stage, err := newTenantStage(util.Logger, testData.config)

			if testData.expectedErr != nil {
				assert.EqualError(t, err, *testData.expectedErr)
				assert.Nil(t, stage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, stage)
			}
		})
	}
}

func TestTenantStage_Process(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config         *TenantConfig
		inputLabels    model.LabelSet
		inputExtracted map[string]interface{}
		expectedTenant *string
	}{
		"should not set the tenant if the source field is not defined in the extracted map": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{},
			inputExtracted: map[string]interface{}{},
			expectedTenant: nil,
		},
		"should not override the tenant if the source field is not defined in the extracted map": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{client.ReservedLabelTenantID: "foo"},
			inputExtracted: map[string]interface{}{},
			expectedTenant: lokiutil.StringRef("foo"),
		},
		"should set the tenant if the source field is defined in the extracted map": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{},
			inputExtracted: map[string]interface{}{"tenant_id": "bar"},
			expectedTenant: lokiutil.StringRef("bar"),
		},
		"should override the tenant if the source field is defined in the extracted map": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{client.ReservedLabelTenantID: "foo"},
			inputExtracted: map[string]interface{}{"tenant_id": "bar"},
			expectedTenant: lokiutil.StringRef("bar"),
		},
		"should not set the tenant if the source field data type can't be converted to string": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{},
			inputExtracted: map[string]interface{}{"tenant_id": []string{"bar"}},
			expectedTenant: nil,
		},
		"should set the tenant with the configured static value": {
			config:         &TenantConfig{Value: "bar"},
			inputLabels:    model.LabelSet{},
			inputExtracted: map[string]interface{}{},
			expectedTenant: lokiutil.StringRef("bar"),
		},
		"should override the tenant with the configured static value": {
			config:         &TenantConfig{Value: "bar"},
			inputLabels:    model.LabelSet{client.ReservedLabelTenantID: "foo"},
			inputExtracted: map[string]interface{}{},
			expectedTenant: lokiutil.StringRef("bar"),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			stage, err := newTenantStage(util.Logger, testData.config)
			require.NoError(t, err)

			// Process and dummy line and ensure nothing has changed except
			// the tenant reserved label
			timestamp := time.Unix(1, 1)
			entry := "hello world"
			labels := testData.inputLabels.Clone()
			extracted := testData.inputExtracted

			stage.Process(labels, extracted, &timestamp, &entry)

			assert.Equal(t, time.Unix(1, 1), timestamp)
			assert.Equal(t, "hello world", entry)

			actualTenant, ok := labels[client.ReservedLabelTenantID]
			if testData.expectedTenant == nil {
				assert.False(t, ok)
			} else {
				assert.Equal(t, *testData.expectedTenant, string(actualTenant))
			}
		})
	}
}
