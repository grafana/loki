package manifests

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"testing"

	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusRule(t *testing.T) {
	opts := Options{
		Stack: v1beta1.LokiStackSpec{
			Size: v1beta1.SizeOneXSmall,
		},
	}

	pr, err := NewPrometheusRule(opts)

	require.NoError(t, err)
	require.NotEmpty(t, pr)
}

func TestGetRuleSpec_CacheValue(t *testing.T) {
	numCalls := 0
	fakeRule := &monitoringv1.PrometheusRuleSpec{}
	fakeNewAlertsSpec := func() (*monitoringv1.PrometheusRuleSpec, error) {
		numCalls++
		return fakeRule, nil
	}

	// Reset the cache that might be set by other tests.
	cachedRuleSpec = nil

	// The first invocation of getRuleSpec() should call fakeNewAlertsSpec() and cache its value
	rule, err := getRuleSpec(fakeNewAlertsSpec)
	require.NoError(t, err)
	require.Equal(t, rule, fakeRule)
	require.Equal(t, 1, numCalls)

	// The second invocation of getRuleSpec() shouldn't call fakeNewAlertsSpec() and return the cached value
	rule, err = getRuleSpec(fakeNewAlertsSpec)
	require.NoError(t, err)
	require.Equal(t, rule, fakeRule)
	require.Equal(t, 1, numCalls)
}
