package bloomgateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/bloomutils"
	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/validation"
)

func TestBloomGatewayClient(t *testing.T) {
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	l, err := validation.NewOverrides(validation.Limits{BloomGatewayShardSize: 1, BloomGatewayEnabled: true}, nil)
	require.NoError(t, err)

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	t.Run("FilterChunks returns response", func(t *testing.T) {
		c, err := NewClient(cfg, &mockRing{}, l, reg, logger, "loki", nil, false)
		require.NoError(t, err)
		res, err := c.FilterChunks(context.Background(), "tenant", model.Now(), model.Now(), nil)
		require.NoError(t, err)
		require.Equal(t, []*logproto.GroupedChunkRefs{}, res)
	})
}

func TestBloomGatewayClient_PartitionFingerprintsByAddresses(t *testing.T) {
	// Create 10 fingerprints [0, 2, 4, ... 18]
	groups := make([]*logproto.GroupedChunkRefs, 0, 10)
	for i := 0; i < 20; i += 2 {
		groups = append(groups, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
	}

	// instance token ranges do not overlap
	t.Run("non-overlapping", func(t *testing.T) {

		servers := []addrsWithBounds{
			{id: "instance-1", addrs: []string{"10.0.0.1"}, FingerprintBounds: v1.NewBounds(0, 4)},
			{id: "instance-2", addrs: []string{"10.0.0.2"}, FingerprintBounds: v1.NewBounds(5, 9)},
			{id: "instance-3", addrs: []string{"10.0.0.3"}, FingerprintBounds: v1.NewBounds(10, 14)},
			{id: "instance-2", addrs: []string{"10.0.0.2"}, FingerprintBounds: v1.NewBounds(15, 19)},
		}

		// partition fingerprints

		expected := []instanceWithFingerprints{
			{
				instance: servers[0],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 0},
					{Fingerprint: 2},
					{Fingerprint: 4},
				},
			},
			{
				instance: servers[1],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 6},
					{Fingerprint: 8},
				},
			},
			{
				instance: servers[2],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 10},
					{Fingerprint: 12},
					{Fingerprint: 14},
				},
			},
			{
				instance: servers[3],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 16},
					{Fingerprint: 18},
				},
			},
		}

		bounded := partitionFingerprintsByAddresses(groups, servers)
		require.Equal(t, expected, bounded)

		// group fingerprints by instance

		expected = []instanceWithFingerprints{
			{
				instance: addrsWithBounds{id: "instance-1", addrs: []string{"10.0.0.1"}},
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 0},
					{Fingerprint: 2},
					{Fingerprint: 4},
				},
			},
			{
				instance: addrsWithBounds{id: "instance-2", addrs: []string{"10.0.0.2"}},
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 6},
					{Fingerprint: 8},
					{Fingerprint: 16},
					{Fingerprint: 18},
				},
			},
			{
				instance: addrsWithBounds{id: "instance-3", addrs: []string{"10.0.0.3"}},
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 10},
					{Fingerprint: 12},
					{Fingerprint: 14},
				},
			},
		}
		result := groupByInstance(bounded)
		require.Equal(t, expected, result)
	})

	// instance token ranges overlap
	t.Run("overlapping", func(t *testing.T) {
		servers := []addrsWithBounds{
			{id: "instance-1", addrs: []string{"10.0.0.1"}, FingerprintBounds: v1.NewBounds(0, 9)},
			{id: "instance-2", addrs: []string{"10.0.0.2"}, FingerprintBounds: v1.NewBounds(5, 14)},
			{id: "instance-3", addrs: []string{"10.0.0.3"}, FingerprintBounds: v1.NewBounds(10, 19)},
		}

		// partition fingerprints

		expected := []instanceWithFingerprints{
			{
				instance: servers[0],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 0},
					{Fingerprint: 2},
					{Fingerprint: 4},
					{Fingerprint: 6},
					{Fingerprint: 8},
				},
			},
			{
				instance: servers[1],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 6},
					{Fingerprint: 8},
					{Fingerprint: 10},
					{Fingerprint: 12},
					{Fingerprint: 14},
				},
			},
			{
				instance: servers[2],
				fingerprints: []*logproto.GroupedChunkRefs{
					{Fingerprint: 10},
					{Fingerprint: 12},
					{Fingerprint: 14},
					{Fingerprint: 16},
					{Fingerprint: 18},
				},
			},
		}

		bounded := partitionFingerprintsByAddresses(groups, servers)
		require.Equal(t, expected, bounded)
	})
}

func BenchmarkPartitionFingerprintsByAddresses(b *testing.B) {
	numFp := 100000
	fpStep := math.MaxUint64 / uint64(numFp)

	groups := make([]*logproto.GroupedChunkRefs, 0, numFp)
	for i := uint64(0); i < math.MaxUint64-fpStep; i += fpStep {
		groups = append(groups, &logproto.GroupedChunkRefs{Fingerprint: i})
	}

	numServers := 100
	tokenStep := math.MaxUint32 / uint32(numServers)
	servers := make([]addrsWithBounds, 0, numServers)
	for i := uint32(0); i < math.MaxUint32-tokenStep; i += tokenStep {
		servers = append(servers, addrsWithBounds{
			id:    fmt.Sprintf("instance-%x", i),
			addrs: []string{fmt.Sprintf("%d", i)},
			FingerprintBounds: v1.NewBounds(
				model.Fingerprint(i)<<32,
				model.Fingerprint(i+tokenStep)<<32,
			),
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = partitionFingerprintsByAddresses(groups, servers)
	}
}

func TestBloomGatewayClient_MapTokenRangeToFingerprintRange(t *testing.T) {
	testCases := map[string]struct {
		lshift int
		inp    bloomutils.Range[uint32]
		exp    v1.FingerprintBounds
	}{
		"single token expands to multiple fingerprints": {
			inp: bloomutils.NewTokenRange(0, 0),
			exp: v1.NewBounds(0, 0xffffffff),
		},
		"max value expands to max value of new range": {
			inp: bloomutils.NewTokenRange((1 << 31), math.MaxUint32),
			exp: v1.NewBounds((1 << 63), 0xffffffffffffffff),
		},
	}
	for desc, tc := range testCases {
		t.Run(desc, func(t *testing.T) {
			actual := mapTokenRangeToFingerprintRange(tc.inp)
			require.Equal(t, tc.exp, actual)
		})
	}
}

func TestBloomGatewayClient_ServerAddressesWithTokenRanges(t *testing.T) {
	testCases := map[string]struct {
		instances []ring.InstanceDesc
		expected  []addrsWithBounds
	}{
		"one token per instance, no gaps between fingerprint ranges": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{(1 << 30) * 1}}, // 0x40000000
				{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{(1 << 30) * 2}}, // 0x80000000
				{Id: "instance-3", Addr: "10.0.0.3", Tokens: []uint32{(1 << 30) * 3}}, // 0xc0000000
			},
			expected: []addrsWithBounds{
				{id: "instance-1", addrs: []string{"10.0.0.1"}, FingerprintBounds: v1.NewBounds(0, 4611686022722355199)},
				{id: "instance-2", addrs: []string{"10.0.0.2"}, FingerprintBounds: v1.NewBounds(4611686022722355200, 9223372041149743103)},
				{id: "instance-3", addrs: []string{"10.0.0.3"}, FingerprintBounds: v1.NewBounds(9223372041149743104, 13835058059577131007)},
				{id: "instance-1", addrs: []string{"10.0.0.1"}, FingerprintBounds: v1.NewBounds(13835058059577131008, 18446744073709551615)},
			},
		},
		"MinUint32 and MaxUint32 are actual tokens in the ring": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{0}},
				{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{math.MaxUint32}},
			},
			expected: []addrsWithBounds{
				{id: "instance-1", addrs: []string{"10.0.0.1"}, FingerprintBounds: v1.NewBounds(0, (1<<32)-1)},
				{id: "instance-2", addrs: []string{"10.0.0.2"}, FingerprintBounds: v1.NewBounds((1 << 32), math.MaxUint64)},
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
	instances := []ring.InstanceDesc{
		{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{0x1fffffff, 0x7fffffff}},
		{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{0x3fffffff, 0x9fffffff}},
		{Id: "instance-3", Addr: "10.0.0.3", Tokens: []uint32{0x5fffffff, 0xbfffffff}},
	}

	subRing := newMockRing(instances)
	servers, err := serverAddressesWithTokenRanges(subRing, instances)
	require.NoError(t, err)

	// for _, s := range servers {
	// 	t.Log(s, v1.NewBounds(model.Fingerprint(s.fpRange.Min), model.Fingerprint(s.fpRange.Max)))
	// }
	/**
	    {instance-1 [10.0.0.1] {         0  536870911} {                   0  2305843004918726656}} 0000000000000000-1fffffff00000000
	    {instance-2 [10.0.0.2] { 536870912 1073741823} { 2305843009213693952  4611686014132420608}} 2000000000000000-3fffffff00000000
	    {instance-3 [10.0.0.3] {1073741824 1610612735} { 4611686018427387904  6917529023346114560}} 4000000000000000-5fffffff00000000
	    {instance-1 [10.0.0.1] {1610612736 2147483647} { 6917529027641081856  9223372032559808512}} 6000000000000000-7fffffff00000000
	    {instance-2 [10.0.0.2] {2147483648 2684354559} { 9223372036854775808 11529215041773502464}} 8000000000000000-9fffffff00000000
	    {instance-3 [10.0.0.3] {2684354560 3221225471} {11529215046068469760 13835058050987196416}} a000000000000000-bfffffff00000000
	    {instance-1 [10.0.0.1] {3221225472 4294967295} {13835058055282163712 18446744073709551615}} c000000000000000-ffffffffffffffff
		**/

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
				{Fingerprint: 0x5000000000000001},
				{Fingerprint: 0x5000000000000010},
				{Fingerprint: 0x5000000000000100},
			},
			expected: []instanceWithFingerprints{
				{
					instance: addrsWithBounds{
						id:    "instance-3",
						addrs: []string{"10.0.0.3"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x5000000000000001},
						{Fingerprint: 0x5000000000000010},
						{Fingerprint: 0x5000000000000100},
					},
				},
			},
		},
		{
			name: "fingerprints within multiple token ranges of a single instance are grouped",
			chunks: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x1000000000000000},
				{Fingerprint: 0x7000000000000000},
				{Fingerprint: 0xd000000000000000},
			},
			expected: []instanceWithFingerprints{
				{
					instance: addrsWithBounds{
						id:    "instance-1",
						addrs: []string{"10.0.0.1"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x1000000000000000},
						{Fingerprint: 0x7000000000000000},
						{Fingerprint: 0xd000000000000000},
					},
				},
			},
		},
		{
			name: "fingerprints with token ranges of multiple instances are grouped",
			chunks: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x1000000000000000},
				{Fingerprint: 0x3000000000000000},
				{Fingerprint: 0x5000000000000000},
				{Fingerprint: 0x7000000000000000},
				{Fingerprint: 0x9000000000000000},
				{Fingerprint: 0xb000000000000000},
				{Fingerprint: 0xd000000000000000},
				{Fingerprint: 0xf000000000000000},
			},
			expected: []instanceWithFingerprints{
				{
					instance: addrsWithBounds{
						id:    "instance-1",
						addrs: []string{"10.0.0.1"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x1000000000000000},
						{Fingerprint: 0x7000000000000000},
						{Fingerprint: 0xd000000000000000},
						{Fingerprint: 0xf000000000000000},
					},
				},
				{
					instance: addrsWithBounds{
						id:    "instance-2",
						addrs: []string{"10.0.0.2"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x3000000000000000},
						{Fingerprint: 0x9000000000000000},
					},
				},
				{
					instance: addrsWithBounds{
						id:    "instance-3",
						addrs: []string{"10.0.0.3"},
					},
					fingerprints: []*logproto.GroupedChunkRefs{
						{Fingerprint: 0x5000000000000000},
						{Fingerprint: 0xb000000000000000},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// sort chunks here, to be able to write more human readable test input
			sort.Slice(tc.chunks, func(i, j int) bool {
				return tc.chunks[i].Fingerprint < tc.chunks[j].Fingerprint
			})
			res := groupFingerprintsByServer(tc.chunks, servers)
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
		if r.ranges[i].TokenRange.Max < key {
			return 1
		}
		if r.ranges[i].TokenRange.Max > key {
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
func (r *mockRing) ShuffleShard(_ string, _ int) ring.ReadRing {
	return r
}

// ShuffleShardWithLookback implements ring.ReadRing.
func (*mockRing) ShuffleShardWithLookback(_ string, _ int, _ time.Duration, _ time.Time) ring.ReadRing {
	panic("unimplemented")
}

// CleanupShuffleShardCache implements ring.ReadRing.
func (*mockRing) CleanupShuffleShardCache(_ string) {
	panic("unimplemented")
}

func (r *mockRing) GetTokenRangesForInstance(_ string) (ring.TokenRanges, error) {
	tr := ring.TokenRanges{0, math.MaxUint32}
	return tr, nil
}
