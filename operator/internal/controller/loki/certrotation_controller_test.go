package loki

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestCertRotationController_RegistersCustomResource_WithDefaultPredicates(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &CertRotationReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)
	b.NamedReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	// Require only one For-Call for the custom resource
	require.Equal(t, 1, b.ForCallCount())

	// Require For-call with LokiStack resource
	obj, _ := b.ForArgsForCall(0)
	require.Equal(t, &lokiv1.LokiStack{}, obj)
}

func TestCertRotationController_RegisterOwnedResources_WithDefaultPredicates(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &CertRotationReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)
	b.NamedReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.OwnsCallCount())

	obj, _ := b.OwnsArgsForCall(0)
	require.Equal(t, &corev1.Secret{}, obj)
}

func TestCertRotationController_ExpiryRetryAfter(t *testing.T) {
	tt := []struct {
		desc         string
		refresh      time.Duration
		wantDuration time.Duration
		wantError    bool
	}{
		{
			desc:         "multi-day refresh durarion",
			refresh:      120 * time.Hour,
			wantDuration: 12 * time.Hour,
		},
		{
			desc:         "less than a day refresh duration",
			refresh:      10 * time.Hour,
			wantDuration: 2*time.Hour + 30*time.Minute,
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.wantDuration, expiryRetryAfter(tc.refresh))
		})
	}
}
