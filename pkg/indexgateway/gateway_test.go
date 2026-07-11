package indexgateway

import (
	"context"
	"math"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	tsdb_index "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_test "github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type mockLimits struct {
	shardSize        int
	maxCapacity      float64
	maxBytesPerShard int
	precomputeChunks bool
}

func (l mockLimits) IndexGatewayShardSize(_ string) int       { return l.shardSize }
func (l mockLimits) IndexGatewayMaxCapacity(_ string) float64 { return l.maxCapacity }
func (l mockLimits) TSDBMaxBytesPerShard(_ string) int        { return l.maxBytesPerShard }
func (l mockLimits) TSDBPrecomputeChunks(_ string) bool       { return l.precomputeChunks }

func TestVolume(t *testing.T) {
	indexQuerier := newIngesterQuerierMock()
	indexQuerier.On("Volume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&logproto.VolumeResponse{Volumes: []logproto.Volume{
		{Name: "bar", Volume: 38},
	}}, nil)

	gateway, err := NewIndexGateway(Config{}, mockLimits{}, util_log.Logger, nil, indexQuerier, nil, nil)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	vol, err := gateway.GetVolume(ctx, &logproto.VolumeRequest{Matchers: "{}"})
	require.NoError(t, err)

	require.Equal(t, &logproto.VolumeResponse{Volumes: []logproto.Volume{
		{Name: "bar", Volume: 38},
	}}, vol)
}

type indexQuerierMock struct {
	IndexQuerier
	util_test.ExtendedMock
}

func newIngesterQuerierMock() *indexQuerierMock {
	return &indexQuerierMock{}
}

func (i *indexQuerierMock) Volume(_ context.Context, userID string, from, through model.Time, _ int32, _ []string, _ string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := i.Called(userID, from, through, matchers)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
}

// Tests for various cases of the `refWithSizingInfo.Cmp` function
func TestRefWithSizingInfo(t *testing.T) {
	for _, tc := range []struct {
		desc string
		a    refWithSizingInfo
		b    tsdb_index.ChunkMeta
		exp  v2.Ord
	}{
		{
			desc: "less by from",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					From: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				MinTime: 2,
			},
			exp: v2.Less,
		},
		{
			desc: "eq by from",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					From: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				MinTime: 1,
			},
			exp: v2.Eq,
		},
		{
			desc: "gt by from",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					From: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				MinTime: 1,
			},
			exp: v2.Greater,
		},
		{
			desc: "less by through",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Through: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				MaxTime: 2,
			},
			exp: v2.Less,
		},
		{
			desc: "eq by through",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Through: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				MaxTime: 2,
			},
			exp: v2.Eq,
		},
		{
			desc: "gt by through",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Through: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				MaxTime: 1,
			},
			exp: v2.Greater,
		},
		{
			desc: "less by checksum",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Checksum: 1,
				},
			},
			b: tsdb_index.ChunkMeta{
				Checksum: 2,
			},
			exp: v2.Less,
		},
		{
			desc: "eq by checksum",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Checksum: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				Checksum: 2,
			},
			exp: v2.Eq,
		},
		{
			desc: "gt by checksum",
			a: refWithSizingInfo{
				ref: &logproto.ChunkRef{
					Checksum: 2,
				},
			},
			b: tsdb_index.ChunkMeta{
				Checksum: 1,
			},
			exp: v2.Greater,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.a.Cmp(tc.b))
		})
	}
}

// TODO(owen-d): more testing for specific cases
func TestAccumulateChunksToShards(t *testing.T) {
	// only check eq by checksum for convenience -- we're not testing the comparison function here
	mkRef := func(fp model.Fingerprint, checksum uint32) logproto.ChunkRef {
		return logproto.ChunkRef{
			Fingerprint: uint64(fp),
			Checksum:    checksum,
		}
	}

	sized := func(ref logproto.ChunkRef, kb, entries uint32) logproto.ChunkRefWithSizingInfo {
		return logproto.ChunkRefWithSizingInfo{
			ChunkRef: ref,
			KB:       kb,
			Entries:  entries,
		}

	}

	filtered := []logproto.ChunkRefWithSizingInfo{
		// shard 0
		sized(mkRef(1, 0), 100, 1),
		sized(mkRef(1, 1), 100, 1),
		sized(mkRef(1, 2), 100, 1),

		// shard 1
		sized(mkRef(2, 10), 100, 1),
		sized(mkRef(2, 20), 100, 1),
		sized(mkRef(2, 30), 100, 1),

		// shard 2 split across multiple series
		sized(mkRef(3, 10), 50, 1),
		sized(mkRef(4, 10), 30, 1),
		sized(mkRef(4, 20), 30, 1),

		// last shard contains leftovers + skip a few fps in between
		sized(mkRef(7, 10), 25, 1),
	}

	shards, grps, err := accumulateChunksToShards(&logproto.ShardsRequest{
		TargetBytesPerShard: 100 << 10,
	}, filtered)

	expectedChks := [][]logproto.ChunkRefWithSizingInfo{
		filtered[0:3],
		filtered[3:6],
		filtered[6:9],
		filtered[9:10],
	}
	exp := []logproto.Shard{
		{
			Bounds: logproto.FPBounds{Min: 0, Max: 1},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  3,
				Entries: 3,
				Bytes:   300 << 10,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: 2, Max: 2},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  3,
				Entries: 3,
				Bytes:   300 << 10,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: 3, Max: 6},
			Stats: &logproto.IndexStatsResponse{
				Streams: 2,
				Chunks:  3,
				Entries: 3,
				Bytes:   110 << 10,
			},
		},
		{
			Bounds: logproto.FPBounds{Min: 7, Max: math.MaxUint64},
			Stats: &logproto.IndexStatsResponse{
				Streams: 1,
				Chunks:  1,
				Entries: 1,
				Bytes:   25 << 10,
			},
		},
	}

	require.NoError(t, err)

	for i := range shards {
		require.Equal(t, exp[i], shards[i], "invalid shard at index %d", i)
		for j := range grps[i].Refs {
			require.Equal(t, &expectedChks[i][j].ChunkRef, grps[i].Refs[j], "invalid chunk in grp %d at index %d", i, j)
		}
	}
	require.Equal(t, len(exp), len(shards))

}
