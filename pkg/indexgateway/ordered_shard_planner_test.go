package indexgateway_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

func TestOrderedShardPlannerParity(t *testing.T) {
	ref := func(fp uint64, from model.Time, checksum uint32) logproto.ChunkRefWithSizingInfo {
		return logproto.ChunkRefWithSizingInfo{
			ChunkRef: logproto.ChunkRef{Fingerprint: fp, UserID: "synthetic", From: from, Through: from + 1, Checksum: checksum},
			KB:       64,
			Entries:  4,
		}
	}
	skewed := make([]logproto.ChunkRefWithSizingInfo, 4096)
	for i := range skewed {
		skewed[i] = ref(2, model.Time(i), 1)
	}
	req := &logproto.ShardsRequest{TargetBytesPerShard: 512 << 10}
	for _, tc := range []struct {
		name  string
		files [][]logproto.ChunkRefWithSizingInfo
	}{
		{name: "empty", files: [][]logproto.ChunkRefWithSizingInfo{{}, {}}},
		{name: "overlapping files", files: [][]logproto.ChunkRefWithSizingInfo{
			{ref(2, -1, 1), ref(2, -1, 2), ref(7, 0, 9), ref(math.MaxUint64, 0, 1)},
			{ref(0, -1, 1), ref(2, -1, 1), ref(2, 0, 1), ref(math.MaxUint64, 0, 1)},
			{ref(0, -1, 1), ref(7, 0, 9)},
		}},
		{name: "large stream across files", files: [][]logproto.ChunkRefWithSizingInfo{
			skewed,
			append(append([]logproto.ChunkRefWithSizingInfo(nil), skewed[512:]...), ref(7, 0, 9)),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var refs []logproto.ChunkRefWithSizingInfo
			for _, file := range tc.files {
				refs = append(refs, file...)
			}
			slices.SortFunc(refs, func(a, b logproto.ChunkRefWithSizingInfo) int {
				if a.Less(b.ChunkRef) {
					return -1
				}
				if b.Less(a.ChunkRef) {
					return 1
				}
				return 0
			})
			refs = slices.CompactFunc(refs, func(a, b logproto.ChunkRefWithSizingInfo) bool {
				return a.ChunkRef == b.ChunkRef
			})
			for _, precompute := range []bool{false, true} {
				opened := make([]*indexgateway.TestChunkCursor, 0, len(tc.files))
				cursors := make([]sharding.ChunkCursor, 0, len(tc.files))
				for _, file := range tc.files {
					cursor := &indexgateway.TestChunkCursor{Refs: file}
					opened = append(opened, cursor)
					cursors = append(cursors, cursor)
				}
				want, err := indexgateway.BuildShardsResponseForTest(req, refs, precompute)
				require.NoError(t, err)
				got, err := indexgateway.PlanShardsFromCursorsForTest(t.Context(), req, precompute, cursors)
				require.NoError(t, err)
				require.Equal(t, want, got)
				for _, cursor := range opened {
					require.True(t, cursor.Closed)
				}
			}
		})
	}
}

func TestOrderedShardPlannerErrorsCloseEveryCursor(t *testing.T) {
	sentinel := errors.New("failed reading index")
	for _, tc := range []struct {
		name   string
		ctx    func(context.Context) context.Context
		cursor *indexgateway.TestChunkCursor
		err    error
	}{
		{name: "index error", cursor: &indexgateway.TestChunkCursor{ReadErr: sentinel}, err: sentinel},
		{name: "out of order", cursor: &indexgateway.TestChunkCursor{Refs: []logproto.ChunkRefWithSizingInfo{
			{ChunkRef: logproto.ChunkRef{Fingerprint: 1, From: 10}},
			{ChunkRef: logproto.ChunkRef{Fingerprint: 1, From: 0}},
		}}, err: indexgateway.ErrUnorderedChunkCursorForTest},
		{name: "canceled", ctx: func(ctx context.Context) context.Context {
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			return canceled
		}, cursor: &indexgateway.TestChunkCursor{}, err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.ctx != nil {
				ctx = tc.ctx(ctx)
			}
			other := &indexgateway.TestChunkCursor{}
			got, err := indexgateway.PlanShardsFromCursorsForTest(ctx, &logproto.ShardsRequest{}, true, []sharding.ChunkCursor{tc.cursor, other})
			require.ErrorIs(t, err, tc.err)
			require.Nil(t, got)
			require.True(t, tc.cursor.Closed)
			require.True(t, other.Closed)
		})
	}
}

func TestOrderedShardPlannerFingerprintCollisions(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts index.ReaderOptions
	}{
		{name: "mmap", opts: index.MmapOptions{}},
		{name: "stream", opts: index.DefaultStreamOptions()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var files tsdb.IndexSlice
			for i, allSeries := range [][]struct {
				fp   model.Fingerprint
				keys []uint32
			}{
				{{5, []uint32{2, 6}}, {5, []uint32{1, 4, 6}}, {5, []uint32{3, 5}}, {9, []uint32{3}}},
				{{5, []uint32{1, 3, 4, 7}}, {9, []uint32{2, 3}}},
			} {
				builder := tsdb.NewBuilder(index.FormatV3)
				for j, series := range allSeries {
					var chunks []index.ChunkMeta
					for _, key := range series.keys {
						// Include equal times with different checksums.
						start := int64(key / 2)
						kb := uint32(64)
						if i == 0 && j == 1 && key == 6 {
							// The first series wins when a duplicate ref has different sizing.
							kb = 128
						}
						chunks = append(chunks, index.ChunkMeta{MinTime: start, MaxTime: start + 4, Checksum: key, KB: kb, Entries: 2})
					}
					builder.AddSeries(labels.FromStrings("app", "synthetic", "stream", string(rune('a'+j))), series.fp, chunks)
				}
				id, err := builder.Build(t.Context(), dir, func(from, through model.Time, checksum uint32) tsdb.Identifier {
					return tsdb.NewPrefixedIdentifier(tsdb.SingleTenantTSDBIdentifier{
						TS: time.Unix(int64(i+1), 0), From: from, Through: through, Checksum: checksum,
					}, dir, dir)
				})
				require.NoError(t, err)
				file, err := tsdb.NewShippableTSDBFile(id, tc.opts)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, file.Close()) })
				files = append(files, file)
			}

			req := &logproto.ShardsRequest{From: 0, Through: 20, TargetBytesPerShard: 512 << 10}
			matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "synthetic")}
			client := tsdb.NewIndexClient(tsdb.NewMultiIndex(files), tsdb.IndexClientOptions{}, nil)
			refs, err := client.GetChunkRefsWithSizingInfo(t.Context(), "synthetic", req.From, req.Through, chunk.Predicate{Matchers: matchers})
			require.NoError(t, err)
			for _, precompute := range []bool{false, true} {
				want, err := indexgateway.BuildShardsResponseForTest(req, refs, precompute)
				require.NoError(t, err)
				cursors, err := client.OpenChunkCursors(t.Context(), "synthetic", req.From, req.Through, matchers...)
				require.NoError(t, err)
				require.Len(t, cursors, 2)
				got, err := indexgateway.PlanShardsFromCursorsForTest(t.Context(), req, precompute, cursors)
				require.NoError(t, err)
				require.Equal(t, want, got)
				require.EqualValues(t, 9, got.Statistics.Index.TotalChunks)
			}
		})
	}
}
