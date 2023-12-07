package bloomgateway

import (
	"math"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/bloomutils"
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
		_, err := NewGatewayClient(cfg, l, reg, logger, "loki", nil, false)
		require.NoError(t, err)
	})
}

func TestBloomGatewayClient_PartitionFingerprintsByAddresses(t *testing.T) {
	// instance token ranges do not overlap
	t.Run("non-overlapping", func(t *testing.T) {
		groups := []*logproto.GroupedChunkRefs{
			{Fingerprint: 0},
			{Fingerprint: 100},
			{Fingerprint: 101},
			{Fingerprint: 200},
			{Fingerprint: 201},
			{Fingerprint: 300},
			{Fingerprint: 301},
			{Fingerprint: 400},
			{Fingerprint: 401}, // out of bounds, will be dismissed
		}
		servers := []addrsWithTokenRange{
			{id: "instance-1", addrs: []string{"10.0.0.1"}, minToken: 0, maxToken: 100},
			{id: "instance-2", addrs: []string{"10.0.0.2"}, minToken: 101, maxToken: 200},
			{id: "instance-3", addrs: []string{"10.0.0.3"}, minToken: 201, maxToken: 300},
			{id: "instance-2", addrs: []string{"10.0.0.2"}, minToken: 301, maxToken: 400},
		}

		// partition fingerprints

		expected := []instanceWithFingerprints{
			{
				instance: servers[0],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 0},
					{Fingerprint: 100},
				},
			},
			{
				instance: servers[1],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 101},
					{Fingerprint: 200},
				},
			},
			{
				instance: servers[2],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 201},
					{Fingerprint: 300},
				},
			},
			{
				instance: servers[3],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 301},
					{Fingerprint: 400},
				},
			},
		}

		bounded := partitionFingerprintsByAddresses(groups, servers)
		require.Equal(t, expected, bounded)

		// group fingerprints by instance

		expected = []instanceWithFingerprints{
			{
				instance: addrsWithTokenRange{id: "instance-1", addrs: []string{"10.0.0.1"}},
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 0},
					{Fingerprint: 100},
				},
			},
			{
				instance: addrsWithTokenRange{id: "instance-2", addrs: []string{"10.0.0.2"}},
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 101},
					{Fingerprint: 200},
					{Fingerprint: 301},
					{Fingerprint: 400},
				},
			},
			{
				instance: addrsWithTokenRange{id: "instance-3", addrs: []string{"10.0.0.3"}},
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 201},
					{Fingerprint: 300},
				},
			},
		}
		result := groupByInstance(bounded)
		require.Equal(t, expected, result)
	})

	// instance token ranges overlap
	t.Run("overlapping", func(t *testing.T) {
		groups := []*logproto.GroupedChunkRefs{
			{Fingerprint: 50},
			{Fingerprint: 150},
			{Fingerprint: 250},
			{Fingerprint: 350},
		}
		servers := []addrsWithTokenRange{
			{id: "instance-1", addrs: []string{"10.0.0.1"}, minToken: 0, maxToken: 200},
			{id: "instance-2", addrs: []string{"10.0.0.2"}, minToken: 100, maxToken: 300},
			{id: "instance-3", addrs: []string{"10.0.0.3"}, minToken: 200, maxToken: 400},
		}

		// partition fingerprints

		expected := []instanceWithFingerprints{
			{instance: servers[0], fingerprints: []*logproto.GroupedChunkRefs{
				{Fingerprint: 50},
				{Fingerprint: 150},
			}},
			{instance: servers[1], fingerprints: []*logproto.GroupedChunkRefs{
				{Fingerprint: 150},
				{Fingerprint: 250},
			}},
			{instance: servers[2], fingerprints: []*logproto.GroupedChunkRefs{
				{Fingerprint: 250},
				{Fingerprint: 350},
			}},
		}

		bounded := partitionFingerprintsByAddresses(groups, servers)
		require.Equal(t, expected, bounded)
	})
}

func TestBloomGatewayClient_ServerAddressesWithTokenRanges(t *testing.T) {
	testCases := map[string]struct {
		instances []ring.InstanceDesc
		expected  []addrsWithTokenRange
	}{
		"one token per instance": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{math.MaxUint32 / 6 * 1}},
				{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{math.MaxUint32 / 6 * 3}},
				{Id: "instance-3", Addr: "10.0.0.3", Tokens: []uint32{math.MaxUint32 / 6 * 5}},
			},
			expected: []addrsWithTokenRange{
				{id: "instance-1", addrs: []string{"10.0.0.1"}, minToken: 0, maxToken: math.MaxUint32 / 6 * 1},
				{id: "instance-2", addrs: []string{"10.0.0.2"}, minToken: math.MaxUint32/6*1 + 1, maxToken: math.MaxUint32 / 6 * 3},
				{id: "instance-3", addrs: []string{"10.0.0.3"}, minToken: math.MaxUint32/6*3 + 1, maxToken: math.MaxUint32 / 6 * 5},
				{id: "instance-1", addrs: []string{"10.0.0.1"}, minToken: math.MaxUint32/6*5 + 1, maxToken: math.MaxUint32},
			},
		},
		"MinUint32 and MaxUint32 are tokens in the ring": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{0, math.MaxUint32 / 3 * 2}},
				{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{math.MaxUint32 / 3 * 1, math.MaxUint32}},
			},
			expected: []addrsWithTokenRange{
				{id: "instance-1", addrs: []string{"10.0.0.1"}, minToken: 0, maxToken: 0},
				{id: "instance-2", addrs: []string{"10.0.0.2"}, minToken: 1, maxToken: math.MaxUint32 / 3},
				{id: "instance-1", addrs: []string{"10.0.0.1"}, minToken: math.MaxUint32/3*1 + 1, maxToken: math.MaxUint32 / 3 * 2},
				{id: "instance-2", addrs: []string{"10.0.0.2"}, minToken: math.MaxUint32/3*2 + 1, maxToken: math.MaxUint32},
			},
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			subRing := newMockRing(tc.instances)
			res, err := serverAddressesWithTokenRanges(subRing, tc.instances)
			require.NoError(t, err)
			require.Equal(t, tc.expected, res)
		})
	}

}

func TestBloomGatewayClient_GroupFingerprintsByServer(t *testing.T) {

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	l, err := validation.NewOverrides(validation.Limits{BloomGatewayShardSize: 1}, nil)
	require.NoError(t, err)

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	c, err := NewGatewayClient(cfg, l, reg, logger, "loki", nil, false)
	require.NoError(t, err)

	instances := []ring.InstanceDesc{
		{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{2146405214, 1029997044, 678878693}},
		{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{296463531, 1697323986, 800258284}},
		{Id: "instance-3", Addr: "10.0.0.3", Tokens: []uint32{2014002871, 315617625, 1036168527}},
	}

	it := bloomutils.NewInstanceSortMergeIterator(instances)
	for it.Next() {
		t.Log(it.At().MaxToken, it.At().Instance.Addr)
	}

	testCases := []struct {
		name     string
		chunks   []*logproto.GroupedChunkRefs
		expected []instanceWithFingerprints
	}{
		{
			name:     "empty input yields empty result",
			chunks:   []*logproto.GroupedChunkRefs{},
			expected: []instanceWithFingerprints{},
		},
		{
			name: "fingerprints within a single token range are grouped",
			chunks: []*logproto.GroupedChunkRefs{
				{Fingerprint: 1000000000, Refs: []*logproto.ShortRef{{Checksum: 1}}},
				{Fingerprint: 1000000001, Refs: []*logproto.ShortRef{{Checksum: 2}}},
			},
			expected: []instanceWithFingerprints{
				{
					instance: addrsWithTokenRange{
						id:    "instance-1",
						addrs: []string{"10.0.0.1"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 1000000000, Refs: []*logproto.ShortRef{{Checksum: 1}}},
						{Fingerprint: 1000000001, Refs: []*logproto.ShortRef{{Checksum: 2}}},
					},
				},
			},
		},
		{
			name: "fingerprints within multiple token ranges of a single instance are grouped",
			chunks: []*logproto.GroupedChunkRefs{
				{Fingerprint: 1000000000, Refs: []*logproto.ShortRef{{Checksum: 1}}},
				{Fingerprint: 2100000000, Refs: []*logproto.ShortRef{{Checksum: 2}}},
			},
			expected: []instanceWithFingerprints{
				{
					instance: addrsWithTokenRange{
						id:    "instance-1",
						addrs: []string{"10.0.0.1"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 1000000000, Refs: []*logproto.ShortRef{{Checksum: 1}}},
						{Fingerprint: 2100000000, Refs: []*logproto.ShortRef{{Checksum: 2}}},
					},
				},
			},
		},
		{
			name: "fingerprints with token ranges of multiple instances are grouped",
			chunks: []*logproto.GroupedChunkRefs{
				// instance 1
				{Fingerprint: 1000000000, Refs: []*logproto.ShortRef{{Checksum: 1}}},
				// instance 1
				{Fingerprint: 2100000000, Refs: []*logproto.ShortRef{{Checksum: 2}}},
				// instance 2
				{Fingerprint: 290000000, Refs: []*logproto.ShortRef{{Checksum: 3}}},
				// instance 2 (fingerprint equals instance token)
				{Fingerprint: 800258284, Refs: []*logproto.ShortRef{{Checksum: 4}}},
				// instance 2 (fingerprint greater than greatest token)
				{Fingerprint: 2147483648, Refs: []*logproto.ShortRef{{Checksum: 5}}},
				// instance 3
				{Fingerprint: 1029997045, Refs: []*logproto.ShortRef{{Checksum: 6}}},
			},
			expected: []instanceWithFingerprints{
				{
					instance: addrsWithTokenRange{
						id:    "instance-2",
						addrs: []string{"10.0.0.2"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 290000000, Refs: []*logproto.ShortRef{{Checksum: 3}}},
						{Fingerprint: 800258284, Refs: []*logproto.ShortRef{{Checksum: 4}}},
						{Fingerprint: 2147483648, Refs: []*logproto.ShortRef{{Checksum: 5}}},
					},
				},
				{
					instance: addrsWithTokenRange{
						id:    "instance-1",
						addrs: []string{"10.0.0.1"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 1000000000, Refs: []*logproto.ShortRef{{Checksum: 1}}},
						{Fingerprint: 2100000000, Refs: []*logproto.ShortRef{{Checksum: 2}}},
					},
				},
				{
					instance: addrsWithTokenRange{
						id:    "instance-3",
						addrs: []string{"10.0.0.3"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 1029997045, Refs: []*logproto.ShortRef{{Checksum: 6}}},
					},
				},
			},
		},
	}

	subRing := newMockRing(instances)
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// sort chunks here, to be able to write more human readable test input
			sort.Slice(tc.chunks, func(i, j int) bool {
				return tc.chunks[i].Fingerprint < tc.chunks[j].Fingerprint
			})

			res, err := c.groupFingerprintsByServer(tc.chunks, subRing, instances)
			require.NoError(t, err)
			require.Equal(t, tc.expected, res)
		})
	}
}

// make sure mockRing implements the ring.ReadRing interface
var _ ring.ReadRing = &mockRing{}

func newMockRing(instances []ring.InstanceDesc) *mockRing {
	it := bloomutils.NewInstanceSortMergeIterator(instances)
	ranges := make([]bloomutils.InstanceWithTokenRange, 0)
	for it.Next() {
		ranges = append(ranges, it.At())
	}
	return &mockRing{
		instances: instances,
		ranges:    ranges,
	}
}

type mockRing struct {
	instances []ring.InstanceDesc
	ranges    []bloomutils.InstanceWithTokenRange
}

// Get implements ring.ReadRing.
func (r *mockRing) Get(key uint32, _ ring.Operation, _ []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	idx, _ := sort.Find(len(r.ranges), func(i int) int {
		if r.ranges[i].MaxToken < key {
			return 1
		}
		if r.ranges[i].MaxToken > key {
			return -1
		}
		return 0
	})
	return ring.ReplicationSet{Instances: []ring.InstanceDesc{r.ranges[idx].Instance}}, nil
}

// GetAllHealthy implements ring.ReadRing.
func (r *mockRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{
		Instances: r.instances,
	}, nil
}

// GetInstanceState implements ring.ReadRing.
func (*mockRing) GetInstanceState(_ string) (ring.InstanceState, error) {
	panic("unimplemented")
}

// GetReplicationSetForOperation implements ring.ReadRing.
func (*mockRing) GetReplicationSetForOperation(_ ring.Operation) (ring.ReplicationSet, error) {
	panic("unimplemented")
}

// HasInstance implements ring.ReadRing.
func (*mockRing) HasInstance(_ string) bool {
	panic("unimplemented")
}

// InstancesCount implements ring.ReadRing.
func (r *mockRing) InstancesCount() int {
	return len(r.instances)
}

// ReplicationFactor implements ring.ReadRing.
func (*mockRing) ReplicationFactor() int {
	return 1
}

// ShuffleShard implements ring.ReadRing.
func (*mockRing) ShuffleShard(_ string, _ int) ring.ReadRing {
	panic("unimplemented")
}

// ShuffleShardWithLookback implements ring.ReadRing.
func (*mockRing) ShuffleShardWithLookback(_ string, _ int, _ time.Duration, _ time.Time) ring.ReadRing {
	panic("unimplemented")
}

// CleanupShuffleShardCache implements ring.ReadRing.
func (*mockRing) CleanupShuffleShardCache(_ string) {
	panic("unimplemented")
}
