package oci

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr string
	}{
		{
			name: "instance principal",
			config: Config{
				Provider: "instance-principal",
				Bucket:   "loki-data",
			},
		},
		{
			name: "unsupported provider",
			config: Config{
				Provider: "instance_principal",
				Bucket:   "loki-data",
			},
			expectErr: "unsupported",
		},
		{
			name: "OKE workload identity",
			config: Config{
				Provider: "oke-workload-identity",
				Bucket:   "loki-data",
				Region:   "ap-tokyo-1",
			},
		},
		{
			name: "missing provider",
			config: Config{
				Bucket: "loki-data",
			},
			expectErr: "provider",
		},
		{
			name: "missing bucket",
			config: Config{
				Provider: "instance-principal",
			},
			expectErr: "bucket",
		},
		{
			name: "OKE workload identity without region",
			config: Config{
				Provider: "oke-workload-identity",
				Bucket:   "loki-data",
			},
			expectErr: "region",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()

			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(
				t,
				strings.Contains(strings.ToLower(err.Error()), tc.expectErr),
				"expected error %q to contain %q",
				err.Error(),
				tc.expectErr,
			)
		})
	}
}
