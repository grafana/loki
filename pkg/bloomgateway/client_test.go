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
		streams   []uint64
		chunks    [][]*logproto.ChunkRef
		addresses [][]string
		expected  []chunkRefsByAddrs
	}{
		{
			name:      "empty input yields empty result",
			streams:   []uint64{},
			chunks:    [][]*logproto.ChunkRef{},
			addresses: [][]string{},
			expected:  []chunkRefsByAddrs{},
		},
		{
			name:    "addresses with same elements are grouped into single item",
			streams: []uint64{1, 2, 3},
			chunks: [][]*logproto.ChunkRef{
				{{Fingerprint: 1, Checksum: 1}},
				{{Fingerprint: 2, Checksum: 2}},
				{{Fingerprint: 3, Checksum: 3}},
			},
			addresses: [][]string{
				{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
				{"10.0.0.2", "10.0.0.3", "10.0.0.1"},
				{"10.0.0.3", "10.0.0.1", "10.0.0.2"},
			},
			expected: []chunkRefsByAddrs{
				{
					addrs: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
					refs: []*logproto.ChunkRef{
						{Fingerprint: 1, Checksum: 1},
						{Fingerprint: 2, Checksum: 2},
						{Fingerprint: 3, Checksum: 3},
					},
					streams: []uint64{1, 2, 3},
				},
			},
		},
		{
			name:    "partially overlapping addresses are not grouped together",
			streams: []uint64{1, 2},
			chunks: [][]*logproto.ChunkRef{
				{{Fingerprint: 1, Checksum: 1}},
				{{Fingerprint: 2, Checksum: 2}},
			},
			addresses: [][]string{
				{"10.0.0.1", "10.0.0.2"},
				{"10.0.0.2", "10.0.0.3"},
			},
			expected: []chunkRefsByAddrs{
				{
					addrs: []string{"10.0.0.1", "10.0.0.2"},
					refs: []*logproto.ChunkRef{
						{Fingerprint: 1, Checksum: 1},
					},
					streams: []uint64{1},
				},
				{
					addrs: []string{"10.0.0.2", "10.0.0.3"},
					refs: []*logproto.ChunkRef{
						{Fingerprint: 2, Checksum: 2},
					},
					streams: []uint64{2},
				},
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res := c.groupStreamsByAddr(tc.streams, tc.chunks, tc.addresses)
			require.Equal(t, tc.expected, res)
		})
	}
}
