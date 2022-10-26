package controllers

import (
	"testing"
	"time"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCertRotationController_RegistersCustomResource_WithDefaultPredicates(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &CertRotationReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)

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

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.OwnsCallCount())

	obj, _ := b.OwnsArgsForCall(0)
	require.Equal(t, &corev1.Secret{}, obj)
}

func TestCertRotationController_ExpiryRetryAfter(t *testing.T) {
	tt := []struct {
		desc         string
		caRefresh    string
		certRefesh   string
		wantDuration time.Duration
		wantError    bool
	}{
		{
			desc:      "ca refresh parse error",
			wantError: true,
		},
		{
			desc:      "cert refresh parse error",
			caRefresh: "1h",
			wantError: true,
		},
		{
			desc:         "ca refresh is reference",
			caRefresh:    "240h",
			certRefesh:   "120h",
			wantDuration: 12 * time.Hour,
		},
		{
			desc:         "cert refresh is reference",
			caRefresh:    "120h",
			certRefesh:   "240h",
			wantDuration: 12 * time.Hour,
		},
		{
			desc:         "multi-day reference refresh durarion",
			caRefresh:    "240h",
			certRefesh:   "120h",
			wantDuration: 12 * time.Hour,
		},
		{
			desc:         "less than a day refresh duration",
			caRefresh:    "120h",
			certRefesh:   "10h",
			wantDuration: 2*time.Hour + 30*time.Minute,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			d, err := expiryRetryAfter(tc.caRefresh, tc.certRefesh)

			switch {
			case tc.wantError:
				require.Error(t, err)
			default:
				require.NoError(t, err)
				require.Equal(t, tc.wantDuration, d)
			}
		})
	}
}
