package alerts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuild_OpenShiftFeatureGate(t *testing.T) {
	tests := []struct {
		name                 string
		openShiftEnabled     bool
		expectedGroupNames   []string
		unexpectedGroupNames []string
	}{
		{
			name:             "OpenShift disabled",
			openShiftEnabled: false,
			expectedGroupNames: []string{
				"logging_loki.alerts",
				"logging_loki.rules",
			},
			unexpectedGroupNames: []string{
				"openshift_logging.telemetry",
			},
		},
		{
			name:             "OpenShift enabled",
			openShiftEnabled: true,
			expectedGroupNames: []string{
				"logging_loki.alerts",
				"logging_loki.rules",
				"openshift_logging.telemetry",
			},
			unexpectedGroupNames: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{
				RunbookURL:       RunbookDefaultURL,
				OpenShiftEnabled: tc.openShiftEnabled,
			}

			spec, err := Build(opts)
			require.NoError(t, err)
			require.NotNil(t, spec)

			groupNames := make(map[string]bool)
			for _, group := range spec.Groups {
				groupNames[group.Name] = true
			}

			for _, expectedGroup := range tc.expectedGroupNames {
				require.True(t, groupNames[expectedGroup])
			}

			for _, unexpectedGroup := range tc.unexpectedGroupNames {
				require.False(t, groupNames[unexpectedGroup])
			}
		})
	}
}
