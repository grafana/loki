package stages

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/util"

	lokiutil "github.com/grafana/loki/v3/pkg/util"
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
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptyLabelSourceOrValue),
		},
		"should fail on empty source": {
			config: &TenantConfig{
				Source: "",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptyLabelSourceOrValue),
		},
		"should fail on empty value": {
			config: &TenantConfig{
				Value: "",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptyLabelSourceOrValue),
		},
		"should fail on empty label": {
			config: &TenantConfig{
				Label: "",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageEmptyLabelSourceOrValue),
		},
		"should fail on both source and value set": {
			config: &TenantConfig{
				Source: "tenant",
				Value:  "team-a",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageConflictingLabelSourceAndValue),
		},
		"should fail on both source and label set": {
			config: &TenantConfig{
				Source: "tenant",
				Label:  "team-a",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageConflictingLabelSourceAndValue),
		},
		"should fail on both label and value set": {
			config: &TenantConfig{
				Label: "tenant",
				Value: "team-a",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageConflictingLabelSourceAndValue),
		},
		"should fail on all set": {
			config: &TenantConfig{
				Label:  "tenant",
				Source: "tenant",
				Value:  "team-a",
			},
			expectedErr: lokiutil.StringRef(ErrTenantStageConflictingLabelSourceAndValue),
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			stage, err := newTenantStage(testData.config)

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
			inputLabels:    model.LabelSet{util.ReservedLabelTenantID: "foo"},
			inputExtracted: map[string]interface{}{},
			expectedTenant: lokiutil.StringRef("foo"),
		},
		"should set the tenant if the source field is defined in the extracted map": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{},
			inputExtracted: map[string]interface{}{"tenant_id": "bar"},
			expectedTenant: lokiutil.StringRef("bar"),
		},
		"should set the tenant if the label is defined in the label map": {
			config:         &TenantConfig{Label: "tenant_id"},
			inputLabels:    model.LabelSet{"tenant_id": "bar"},
			inputExtracted: map[string]interface{}{},
			expectedTenant: lokiutil.StringRef("bar"),
		},
		"should override the tenant if the source field is defined in the extracted map": {
			config:         &TenantConfig{Source: "tenant_id"},
			inputLabels:    model.LabelSet{util.ReservedLabelTenantID: "foo"},
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
			inputLabels:    model.LabelSet{util.ReservedLabelTenantID: "foo"},
			inputExtracted: map[string]interface{}{},
			expectedTenant: lokiutil.StringRef("bar"),
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			stage, err := newTenantStage(testData.config)
			require.NoError(t, err)

			// Process and dummy line and ensure nothing has changed except
			// the tenant reserved label

			out := processEntries(stage, newEntry(testData.inputExtracted, testData.inputLabels.Clone(), "hello world", time.Unix(1, 1)))[0]

			assert.Equal(t, time.Unix(1, 1), out.Timestamp)
			assert.Equal(t, "hello world", out.Line)

			actualTenant, ok := out.Labels[util.ReservedLabelTenantID]
			if testData.expectedTenant == nil {
				assert.False(t, ok)
			} else {
				assert.Equal(t, *testData.expectedTenant, string(actualTenant))
			}
		})
	}
}
