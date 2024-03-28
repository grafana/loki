package bloomgateway

import (
	"context"
	"fmt"
	"math"
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
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/plan"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/validation"
)

func rs(id int, tokens ...uint32) ring.ReplicationSet {
	inst := ring.InstanceDesc{
		Id:     fmt.Sprintf("instance-%d", id),
		Addr:   fmt.Sprintf("10.0.0.%d", id),
		Tokens: tokens,
	}
	return ring.ReplicationSet{Instances: []ring.InstanceDesc{inst}}
}

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
		expr, err := syntax.ParseExpr(`{foo="bar"}`)
		require.NoError(t, err)
		res, err := c.FilterChunks(context.Background(), "tenant", model.Now(), model.Now(), nil, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, []*logproto.GroupedChunkRefs{}, res)
	})
}

func TestBloomGatewayClient_ReplicationSetsWithBounds(t *testing.T) {
	testCases := map[string]struct {
		instances []ring.InstanceDesc
		expected  []rsWithRanges
	}{
		"single instance covers full range": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{(1 << 31)}}, // 0x80000000
			},
			expected: []rsWithRanges{
				{rs: rs(1, (1 << 31)), ranges: []v1.FingerprintBounds{
					v1.NewBounds(0, math.MaxUint64),
				}},
			},
		},
		"one token per instance": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{(1 << 30) * 1}}, // 0x40000000
				{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{(1 << 30) * 2}}, // 0x80000000
				{Id: "instance-3", Addr: "10.0.0.3", Tokens: []uint32{(1 << 30) * 3}}, // 0xc0000000
			},
			expected: []rsWithRanges{
				{rs: rs(1, (1<<30)*1), ranges: []v1.FingerprintBounds{
					v1.NewBounds(0, 4611686018427387903),
					v1.NewBounds(13835058055282163712, 18446744073709551615),
				}},
				{rs: rs(2, (1<<30)*2), ranges: []v1.FingerprintBounds{
					v1.NewBounds(4611686018427387904, 9223372036854775807),
				}},
				{rs: rs(3, (1<<30)*3), ranges: []v1.FingerprintBounds{
					v1.NewBounds(9223372036854775808, 13835058055282163711),
				}},
			},
		},
		"extreme tokens in ring": {
			instances: []ring.InstanceDesc{
				{Id: "instance-1", Addr: "10.0.0.1", Tokens: []uint32{0}},
				{Id: "instance-2", Addr: "10.0.0.2", Tokens: []uint32{math.MaxUint32}},
			},
			expected: []rsWithRanges{
				{rs: rs(1, 0), ranges: []v1.FingerprintBounds{
					v1.NewBounds(math.MaxUint64-math.MaxUint32, math.MaxUint64),
				}},
				{rs: rs(2, math.MaxUint32), ranges: []v1.FingerprintBounds{
					v1.NewBounds(0, math.MaxUint64-math.MaxUint32-1),
				}},
			},
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			subRing := newMockRing(t, tc.instances)
			res, err := replicationSetsWithBounds(subRing, tc.instances)
			require.NoError(t, err)
			require.Equal(t, tc.expected, res)
		})
	}
}

func TestBloomGatewayClient_PartitionByReplicationSet(t *testing.T) {
	// Create 10 fingerprints [0, 2, 4, ... 18]
	groups := make([]*logproto.GroupedChunkRefs, 0, 10)
	for i := 0; i < 20; i += 2 {
		groups = append(groups, &logproto.GroupedChunkRefs{Fingerprint: uint64(i)})
	}

	// instance token ranges do not overlap
	t.Run("non-overlapping", func(t *testing.T) {

		servers := []rsWithRanges{
			{rs: rs(1), ranges: []v1.FingerprintBounds{v1.NewBounds(0, 4)}},
			{rs: rs(2), ranges: []v1.FingerprintBounds{v1.NewBounds(5, 9), v1.NewBounds(15, 19)}},
			{rs: rs(3), ranges: []v1.FingerprintBounds{v1.NewBounds(10, 14)}},
		}

		// partition fingerprints

		expected := [][]*logproto.GroupedChunkRefs{
			{
				{Fingerprint: 0},
				{Fingerprint: 2},
				{Fingerprint: 4},
			},
			{
				{Fingerprint: 6},
				{Fingerprint: 8},
				{Fingerprint: 16},
				{Fingerprint: 18},
			},
			{
				{Fingerprint: 10},
				{Fingerprint: 12},
				{Fingerprint: 14},
			},
		}

		partitioned := partitionByReplicationSet(groups, servers)
		for i := range partitioned {
			require.Equal(t, expected[i], partitioned[i].groups)
		}
	})

	// instance token ranges overlap -- this should not happen in a real ring, though
	t.Run("overlapping", func(t *testing.T) {
		servers := []rsWithRanges{
			{rs: rs(1), ranges: []v1.FingerprintBounds{v1.NewBounds(0, 9)}},
			{rs: rs(2), ranges: []v1.FingerprintBounds{v1.NewBounds(5, 14)}},
			{rs: rs(3), ranges: []v1.FingerprintBounds{v1.NewBounds(10, 19)}},
		}

		// partition fingerprints

		expected := [][]*logproto.GroupedChunkRefs{
			{
				{Fingerprint: 0},
				{Fingerprint: 2},
				{Fingerprint: 4},
				{Fingerprint: 6},
				{Fingerprint: 8},
			},
			{
				{Fingerprint: 6},
				{Fingerprint: 8},
				{Fingerprint: 10},
				{Fingerprint: 12},
				{Fingerprint: 14},
			},
			{
				{Fingerprint: 10},
				{Fingerprint: 12},
				{Fingerprint: 14},
				{Fingerprint: 16},
				{Fingerprint: 18},
			},
		}

		partitioned := partitionByReplicationSet(groups, servers)
		for i := range partitioned {
			require.Equal(t, expected[i], partitioned[i].groups)
		}
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
	servers := make([]rsWithRanges, 0, numServers)
	for i := uint32(0); i < math.MaxUint32-tokenStep; i += tokenStep {
		servers = append(servers, rsWithRanges{
			rs: rs(int(i)),
			ranges: []v1.FingerprintBounds{
				v1.NewBounds(model.Fingerprint(i)<<32, model.Fingerprint(i+tokenStep)<<32),
			},
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = partitionByReplicationSet(groups, servers)
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

// make sure mockRing implements the ring.ReadRing interface
var _ ring.ReadRing = &mockRing{}

func newMockRing(t *testing.T, instances []ring.InstanceDesc) *mockRing {
	ranges := make([]ring.TokenRanges, 0)
	for i := range instances {
		tr, err := bloomutils.TokenRangesForInstance(instances[i].Id, instances)
		if err != nil {
			t.Fatal(err)
		}
		ranges = append(ranges, tr)
	}
	return &mockRing{
		instances: instances,
		ranges:    ranges,
	}
}

type mockRing struct {
	instances []ring.InstanceDesc
	ranges    []ring.TokenRanges
}

// Get implements ring.ReadRing.
func (r *mockRing) Get(key uint32, _ ring.Operation, _ []ring.InstanceDesc, _ []string, _ []string) (rs ring.ReplicationSet, err error) {
	for i := range r.ranges {
		if r.ranges[i].IncludesKey(key) {
			rs.Instances = append(rs.Instances, r.instances[i])
		}
	}
	return
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

func (r *mockRing) GetTokenRangesForInstance(id string) (ring.TokenRanges, error) {
	return bloomutils.TokenRangesForInstance(id, r.instances)
}
