package partitionring

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractPartition(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int32
		expectedErr bool
	}{
		{
			name:        "Valid instance returns partition number",
			input:       "instance-5",
			expected:    5,
			expectedErr: false,
		},
		{
			name:        "Local hostname returns 0",
			input:       "instance-local",
			expected:    0,
			expectedErr: false,
		},
		{
			name:        "Invalid format returns error",
			input:       "invalid-format",
			expected:    0,
			expectedErr: true,
		},
		{
			name:        "Invalid partition number returns error",
			input:       "ingester-abc",
			expected:    0,
			expectedErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ExtractPartition(test.input)
			if test.expectedErr {
				require.NotNil(t, err)
				require.Equal(t, int32(0), actual)
			} else {
				require.Nil(t, err)
				require.Equal(t, test.expected, actual)
			}
		})
	}
}
