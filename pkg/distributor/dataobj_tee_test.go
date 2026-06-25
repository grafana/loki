package distributor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataObjTeeConfig_Validate(t *testing.T) {
	tests := []struct {
		name          string
		cfg           DataObjTeeConfig
		expectedError string
	}{
		{
			name: "disabled config skips all checks",
			cfg: DataObjTeeConfig{
				Enabled:               false,
				Topic:                 "",
				MaxBufferedBytes:      -1,
				PerPartitionRateBytes: -1,
			},
		},
		{
			name:          "enabled without topic returns error",
			cfg:           DataObjTeeConfig{Enabled: true},
			expectedError: "the topic is required",
		},
		{
			name: "negative max buffered bytes returns error",
			cfg: DataObjTeeConfig{
				Enabled:               true,
				Topic:                 "foo",
				MaxBufferedBytes:      -1,
				PerPartitionRateBytes: 1024,
			},
			expectedError: "max buffered bytes cannot be negative",
		},
		{
			name: "zero per partition rate bytes returns error",
			cfg: DataObjTeeConfig{
				Enabled:               true,
				Topic:                 "foo",
				PerPartitionRateBytes: 0,
			},
			expectedError: "per partition rate bytes must be positive",
		},
		{
			name: "negative per partition rate bytes returns error",
			cfg: DataObjTeeConfig{
				Enabled:               true,
				Topic:                 "foo",
				PerPartitionRateBytes: -1,
			},
			expectedError: "per partition rate bytes must be positive",
		},
		{
			name: "valid config returns no error",
			cfg: DataObjTeeConfig{
				Enabled:               true,
				Topic:                 "foo",
				MaxBufferedBytes:      0,
				PerPartitionRateBytes: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
