package bloomgateway

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/validation"
)

func TestBloomGatewayClient(t *testing.T) {

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	l, err := validation.NewOverrides(validation.Limits{BloomGatewayShardSize: 1}, nil)
	require.NoError(t, err)

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	t.Run("", func(t *testing.T) {
		_, err := NewGatewayClient(cfg, l, reg, logger)
		require.NoError(t, err)
	})
}

func TestBloomGatewayClient_GroupStreamsByAddresses(t *testing.T) {

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	l, err := validation.NewOverrides(validation.Limits{BloomGatewayShardSize: 1}, nil)
	require.NoError(t, err)

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	c, err := NewGatewayClient(cfg, l, reg, logger)
	require.NoError(t, err)

	testCases := []struct {
		name      string
		chunks    []*logproto.GroupedChunkRefs
		addresses [][]string
		expected  []chunkRefsByAddrs
	}{
		{
			name:      "empty input yields empty result",
			chunks:    []*logproto.GroupedChunkRefs{},
			addresses: [][]string{},
			expected:  []chunkRefsByAddrs{},
		},
		{
			name: "addresses with same elements are grouped into single item",
			chunks: []*logproto.GroupedChunkRefs{
				{Fingerprint: 1, Refs: []*logproto.ShortRef{{Checksum: 1}}},
				{Fingerprint: 2, Refs: []*logproto.ShortRef{{Checksum: 2}}},
				{Fingerprint: 3, Refs: []*logproto.ShortRef{{Checksum: 3}}},
			},
			addresses: [][]string{
				{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
				{"10.0.0.2", "10.0.0.3", "10.0.0.1"},
				{"10.0.0.3", "10.0.0.1", "10.0.0.2"},
			},
			expected: []chunkRefsByAddrs{
				{
					addrs: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
					refs: []*logproto.GroupedChunkRefs{
						{Fingerprint: 1, Refs: []*logproto.ShortRef{{Checksum: 1}}},
						{Fingerprint: 2, Refs: []*logproto.ShortRef{{Checksum: 2}}},
						{Fingerprint: 3, Refs: []*logproto.ShortRef{{Checksum: 3}}},
					},
				},
			},
		},
		{
			name: "partially overlapping addresses are not grouped together",
			chunks: []*logproto.GroupedChunkRefs{
				{Fingerprint: 1, Refs: []*logproto.ShortRef{{Checksum: 1}}},
				{Fingerprint: 2, Refs: []*logproto.ShortRef{{Checksum: 2}}},
			},
			addresses: [][]string{
				{"10.0.0.1", "10.0.0.2"},
				{"10.0.0.2", "10.0.0.3"},
			},
			expected: []chunkRefsByAddrs{
				{
					addrs: []string{"10.0.0.1", "10.0.0.2"},
					refs: []*logproto.GroupedChunkRefs{
						{Fingerprint: 1, Refs: []*logproto.ShortRef{{Checksum: 1}}},
					},
				},
				{
					addrs: []string{"10.0.0.2", "10.0.0.3"},
					refs: []*logproto.GroupedChunkRefs{
						{Fingerprint: 2, Refs: []*logproto.ShortRef{{Checksum: 2}}},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res := c.groupStreamsByAddr(tc.chunks, tc.addresses)
			require.Equal(t, tc.expected, res)
		})
	}
}
