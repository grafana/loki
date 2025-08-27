package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

func TestFeatureChecker_NetworkPoliciesEnabled(t *testing.T) {
	tests := []struct {
		name        string
		np          lokiv1.NetworkPoliciesType
		isOpenShift bool
		version     openshift.OpenShiftVersion
		expected    bool
	}{
		// NetworkPoliciesDisabled - always returns false
		{
			name:        "NetworkPoliciesDisabled with non-OpenShift",
			np:          lokiv1.NetworkPoliciesDisabled,
			isOpenShift: false,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDisabled with OpenShift 4.19",
			np:          lokiv1.NetworkPoliciesDisabled,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 19},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDisabled with OpenShift 4.20",
			np:          lokiv1.NetworkPoliciesDisabled,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDisabled with OpenShift 4.21",
			np:          lokiv1.NetworkPoliciesDisabled,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 21},
			expected:    false,
		},

		// NetworkPoliciesEnabled - always returns true
		{
			name:        "NetworkPoliciesEnabled with non-OpenShift",
			np:          lokiv1.NetworkPoliciesEnabled,
			isOpenShift: false,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    true,
		},
		{
			name:        "NetworkPoliciesEnabled with OpenShift 4.19",
			np:          lokiv1.NetworkPoliciesEnabled,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 19},
			expected:    true,
		},
		{
			name:        "NetworkPoliciesEnabled with OpenShift 4.20",
			np:          lokiv1.NetworkPoliciesEnabled,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    true,
		},
		{
			name:        "NetworkPoliciesEnabled with OpenShift 4.21",
			np:          lokiv1.NetworkPoliciesEnabled,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 21},
			expected:    true,
		},

		// NetworkPoliciesDefault - conditional logic
		{
			name:        "NetworkPoliciesDefault with non-OpenShift",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: false,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDefault with non-OpenShift (higher version)",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: false,
			version:     openshift.OpenShiftVersion{Major: 5, Minor: 0},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDefault with OpenShift 3.11",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 3, Minor: 11},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDefault with OpenShift 4.19",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 19},
			expected:    false,
		},
		{
			name:        "NetworkPoliciesDefault with OpenShift 4.20 (threshold)",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    true,
		},
		{
			name:        "NetworkPoliciesDefault with OpenShift 4.21",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 21},
			expected:    true,
		},
		{
			name:        "NetworkPoliciesDefault with OpenShift 5.0",
			np:          lokiv1.NetworkPoliciesDefault,
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 5, Minor: 0},
			expected:    true,
		},

		// Edge cases - invalid enum values
		{
			name:        "Invalid NetworkPoliciesType with non-OpenShift",
			np:          lokiv1.NetworkPoliciesType("invalid"),
			isOpenShift: false,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    false,
		},
		{
			name:        "Invalid NetworkPoliciesType with OpenShift 4.20",
			np:          lokiv1.NetworkPoliciesType("invalid"),
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    false,
		},
		{
			name:        "Empty NetworkPoliciesType with OpenShift 4.20",
			np:          lokiv1.NetworkPoliciesType(""),
			isOpenShift: true,
			version:     openshift.OpenShiftVersion{Major: 4, Minor: 20},
			expected:    true, // Empty string should be NetworkPoliciesDefault
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featureChecker := NewFeatureGate(tt.isOpenShift, &tt.version)
			result := featureChecker.NetworkPoliciesEnabled(tt.np)
			require.Equal(t, tt.expected, result)
		})
	}
}
