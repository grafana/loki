package openshift

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenShiftRelease_IsAtLeast(t *testing.T) {
	tests := []struct {
		name     string
		version  *OpenShiftRelease
		expected bool
	}{
		{
			name:     "version is higher major",
			version:  &OpenShiftRelease{Major: 5, Minor: 0},
			expected: true,
		},
		{
			name:     "version is same major, higher minor",
			version:  &OpenShiftRelease{Major: 4, Minor: 21},
			expected: true,
		},
		{
			name:     "version is same major and minor",
			version:  &OpenShiftRelease{Major: 4, Minor: 20},
			expected: true,
		},
		{
			name:     "version is lower major",
			version:  &OpenShiftRelease{Major: 3, Minor: 20},
			expected: false,
		},
		{
			name:     "version is same major, lower minor",
			version:  &OpenShiftRelease{Major: 4, Minor: 19},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.version.IsAtLeast(4, 20)
			require.Equal(t, tt.expected, result)
		})
	}
}
