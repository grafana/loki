package distributor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantPrefixCodec(t *testing.T) {
	tests := []struct {
		name        string
		codec       TenantPrefixCodec
		tenant      string
		shard       int32
		encoded     string
		expectedErr string
		decodeOnly  bool // for testing invalid decode cases
	}{
		{
			name:    "simple tenant and prefix",
			codec:   "prefix",
			tenant:  "tenant1",
			shard:   1,
			encoded: "prefix.tenant1.1",
		},
		{
			name:    "tenant with dots",
			codec:   "prefix",
			tenant:  "tenant.with.dots",
			shard:   2,
			encoded: "prefix.tenant.with.dots.2",
		},
		{
			name:    "prefix with dots",
			codec:   "prefix.with.dots",
			tenant:  "tenant1",
			shard:   3,
			encoded: "prefix.with.dots.tenant1.3",
		},
		{
			name:    "both tenant and prefix with dots",
			codec:   "prefix.with.dots",
			tenant:  "tenant.with.dots",
			shard:   4,
			encoded: "prefix.with.dots.tenant.with.dots.4",
		},
		{
			name:        "invalid format - missing prefix",
			codec:       "prefix",
			encoded:     "wrongprefix.tenant1.1",
			decodeOnly:  true,
			expectedErr: "invalid format: wrongprefix.tenant1.1",
		},
		{
			name:        "invalid format - missing shard",
			codec:       "prefix",
			encoded:     "prefix.tenant1",
			decodeOnly:  true,
			expectedErr: "invalid format: prefix.tenant1",
		},
		{
			name:        "invalid format - non-numeric shard",
			codec:       "prefix",
			encoded:     "prefix.tenant1.abc",
			decodeOnly:  true,
			expectedErr: "invalid format: prefix.tenant1.abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.decodeOnly {
				// Test encode
				encoded := tt.codec.Encode(tt.tenant, tt.shard)
				require.Equal(t, tt.encoded, encoded)

				// Test encode/decode roundtrip
				decodedTenant, decodedShard, err := tt.codec.Decode(encoded)
				require.NoError(t, err)
				require.Equal(t, tt.tenant, decodedTenant)
				require.Equal(t, tt.shard, decodedShard)
			} else {
				// Test decode error cases
				tenant, shard, err := tt.codec.Decode(tt.encoded)
				require.Error(t, err)
				require.Equal(t, tt.expectedErr, err.Error())
				require.Equal(t, "", tenant)
				require.Equal(t, int32(0), shard)
			}
		})
	}
}
