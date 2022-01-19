package manifests_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/grafana/loki/operator/internal/manifests/internal/alerts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusRule(t *testing.T) {
	opts := manifests.Options{
		Stack: v1beta1.LokiStackSpec{
			Size: v1beta1.SizeOneXSmall,
		},
	}

	pr, err := manifests.NewPrometheusRule(opts)

	require.NoError(t, err)
	require.NotEmpty(t, pr)
}

func TestAlertsOptions(t *testing.T) {
	opts := manifests.Options{
		Stack: v1beta1.LokiStackSpec{
			Size: v1beta1.SizeOneXSmall,
		},
	}

	expectedAlertsOpts := alerts.Options{
		Stack:                      opts.Stack,
		WritePathHighLoadThreshold: internal.WritePathHighLoadThresholdTable[opts.Stack.Size],
		ReadPathHighLoadThreshold:  internal.ReadPathHighLoadThresholdTable[opts.Stack.Size],
	}

	expected, err := json.Marshal(expectedAlertsOpts)
	require.NoError(t, err)

	res := manifests.AlertsOptions(opts)
	actual, err := json.Marshal(res)

	require.NoError(t, err)
	assert.JSONEq(t, string(expected), string(actual))
}
