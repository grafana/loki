package indexgateway

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	tsdb_index "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	util_test "github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	util_math "github.com/grafana/loki/v3/pkg/util/math"
)

const (
	// query prefixes
	tableNamePrefix        = "table-name"
	hashValuePrefix        = "hash-value"
	rangeValuePrefixPrefix = "range-value-prefix"
	rangeValueStartPrefix  = "range-value-start"
	valueEqualPrefix       = "value-equal"

	// response prefixes
	rangeValuePrefix = "range-value"
	valuePrefix      = "value"
)

type mockLimits struct{}

func (mockLimits) IndexGatewayShardSize(_ string) int {
	return 0
}
func (mockLimits) TSDBMaxBytesPerShard(_ string) int {
	return sharding.DefaultTSDBMaxBytesPerShard
}
func (mockLimits) TSDBPrecomputeChunks(_ string) bool {
	return false
}

type mockBatch struct {
	size int
}

func (r *mockBatch) Iterator() index.ReadBatchIterator {
	return &mockBatchIter{
		curr: -1,
		size: r.size,
	}
}

type mockBatchIter struct {
	curr, size int
}

func (b *mockBatchIter) Next() bool {
	b.curr++
	return b.curr < b.size
}

func (b *mockBatchIter) RangeValue() []byte {
	return []byte(fmt.Sprintf("%s%d", rangeValuePrefix, b.curr))
}

func (b *mockBatchIter) Value() []byte {
	return []byte(fmt.Sprintf("%s%d", valuePrefix, b.curr))
}

type mockQueryIndexServer struct {
	grpc.ServerStream
	callback func(resp *logproto.QueryIndexResponse)
}

func (m *mockQueryIndexServer) Send(resp *logproto.QueryIndexResponse) error {
	m.callback(resp)
	return nil
}

func (m *mockQueryIndexServer) Context() context.Context {
	return context.Background()
}

type mockIndexClient struct {
	index.Client
	response      *mockBatch
	tablesQueried []string
}

func (m *mockIndexClient) QueryPages(_ context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	for _, query := range queries {
		m.tablesQueried = append(m.tablesQueried, query.TableName)
		callback(query, m.response)
	}

	return nil
}

func TestGateway_QueryIndex(t *testing.T) {
	var expectedQueryKey string
	type batchRange struct {
		start, end int
	}
	var expectedRanges []batchRange

	// the response should have index entries between start and end from batchRange at index 0 of expectedRanges
	var server logproto.IndexGateway_QueryIndexServer = &mockQueryIndexServer{
		callback: func(resp *logproto.QueryIndexResponse) {
			require.Equal(t, expectedQueryKey, resp.QueryKey)

			require.True(t, len(expectedRanges) > 0)
			require.Len(t, resp.Rows, expectedRanges[0].end-expectedRanges[0].start)
			i := expectedRanges[0].start
			for _, row := range resp.Rows {
				require.Equal(t, fmt.Sprintf("%s%d", rangeValuePrefix, i), string(row.RangeValue))
				require.Equal(t, fmt.Sprintf("%s%d", valuePrefix, i), string(row.Value))
				i++
			}

			// remove first element for checking the response from next callback.
			expectedRanges = expectedRanges[1:]
		},
	}

	gateway := Gateway{}
	responseSizes := []int{0, 99, maxIndexEntriesPerResponse, 2 * maxIndexEntriesPerResponse, 5*maxIndexEntriesPerResponse - 1}
	for i, responseSize := range responseSizes {
		query := index.Query{
			TableName:        fmt.Sprintf("%s%d", tableNamePrefix, i),
			HashValue:        fmt.Sprintf("%s%d", hashValuePrefix, i),
			RangeValuePrefix: []byte(fmt.Sprintf("%s%d", rangeValuePrefixPrefix, i)),
			RangeValueStart:  []byte(fmt.Sprintf("%s%d", rangeValueStartPrefix, i)),
			ValueEqual:       []byte(fmt.Sprintf("%s%d", valueEqualPrefix, i)),
		}

		// build expectedRanges based on maxIndexEntriesPerResponse
		for j := 0; j < responseSize; j += maxIndexEntriesPerResponse {
			expectedRanges = append(expectedRanges, batchRange{
				start: j,
				end:   util_math.Min(j+maxIndexEntriesPerResponse, responseSize),
			})
		}
		expectedQueryKey = index.QueryKey(query)
		gateway.indexClients = []IndexClientWithRange{{
			IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
			TableRange: config.TableRange{
				Start: 0,
				End:   math.MaxInt64,
				PeriodConfig: &config.PeriodConfig{
					IndexTables: config.IndexPeriodicTableConfig{
						PeriodicTableConfig: config.PeriodicTableConfig{Prefix: tableNamePrefix}},
				},
			},
		}}

		err := gateway.QueryIndex(&logproto.QueryIndexRequest{Queries: []*logproto.IndexQuery{{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		}}}, server)
		require.NoError(t, err)

		// verify that we actually got responses back by checking if expectedRanges got cleared.
		require.Len(t, expectedRanges, 0)
	}
}

func TestGateway_QueryIndex_multistore(t *testing.T) {
	var (
		responseSize    = 99
		expectedQueries []*logproto.IndexQuery
		queries         []*logproto.IndexQuery
	)

	var server logproto.IndexGateway_QueryIndexServer = &mockQueryIndexServer{
		callback: func(resp *logproto.QueryIndexResponse) {
			require.True(t, len(expectedQueries) > 0)
			require.Equal(t, index.QueryKey(index.Query{
				TableName:        expectedQueries[0].TableName,
				HashValue:        expectedQueries[0].HashValue,
				RangeValuePrefix: expectedQueries[0].RangeValuePrefix,
				RangeValueStart:  expectedQueries[0].RangeValueStart,
				ValueEqual:       expectedQueries[0].ValueEqual,
			}), resp.QueryKey)
			require.Len(t, resp.Rows, responseSize)

			expectedQueries = expectedQueries[1:]
		},
	}

	// builds queries for the listed tables
	for _, i := range []int{6, 10, 12, 16, 99} {
		queries = append(queries, &logproto.IndexQuery{
			TableName:        fmt.Sprintf("%s%d", tableNamePrefix, i),
			HashValue:        fmt.Sprintf("%s%d", hashValuePrefix, i),
			RangeValuePrefix: []byte(fmt.Sprintf("%s%d", rangeValuePrefixPrefix, i)),
			RangeValueStart:  []byte(fmt.Sprintf("%s%d", rangeValueStartPrefix, i)),
			ValueEqual:       []byte(fmt.Sprintf("%s%d", valueEqualPrefix, i)),
		})
	}

	indexClients := []IndexClientWithRange{{
		IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
		// no matching queries for this range
		TableRange: config.TableRange{
			Start: 0,
			End:   4,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{Prefix: tableNamePrefix}},
			},
		},
	}, {
		IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
		TableRange: config.TableRange{
			Start: 5,
			End:   10,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{Prefix: tableNamePrefix}},
			},
		},
	}, {
		IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
		TableRange: config.TableRange{
			Start: 15,
			End:   math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{Prefix: tableNamePrefix}},
			},
		},
	}}
	gateway, err := NewIndexGateway(Config{}, mockLimits{}, util_log.Logger, nil, nil, indexClients, nil)
	require.NoError(t, err)

	expectedQueries = append(expectedQueries,
		queries[3], queries[4], // queries matching table range 15->MaxInt64
		queries[0], queries[1], // queries matching table range 5->10
	)

	err = gateway.QueryIndex(&logproto.QueryIndexRequest{Queries: queries}, server)
	require.NoError(t, err)

	// since indexClients are sorted, 0 index would contain the latest period
	require.ElementsMatch(t, gateway.indexClients[0].IndexClient.(*mockIndexClient).tablesQueried, []string{"table-name16", "table-name99"})
	require.ElementsMatch(t, gateway.indexClients[1].IndexClient.(*mockIndexClient).tablesQueried, []string{"table-name6", "table-name10"})
	require.ElementsMatch(t, gateway.indexClients[2].IndexClient.(*mockIndexClient).tablesQueried, []string{})

	require.Len(t, expectedQueries, 0)
}

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
	mkRef := func(fp model.Fingerprint, checksum uint32) *logproto.ChunkRef {
		return &logproto.ChunkRef{
			Fingerprint: uint64(fp),
			Checksum:    checksum,
		}
	}

	sized := func(ref *logproto.ChunkRef, kb, entries uint32) refWithSizingInfo {
		return refWithSizingInfo{
			ref:     ref,
			KB:      kb,
			Entries: entries,
		}

	}

	fsImpl := func(series [][]refWithSizingInfo) sharding.ForSeriesFunc {
		return sharding.ForSeriesFunc(
			func(
				_ context.Context,
				_ string,
				_ tsdb_index.FingerprintFilter,
				_, _ model.Time,
				fn func(
					_ labels.Labels,
					fp model.Fingerprint,
					chks []tsdb_index.ChunkMeta,
				) (stop bool), _ ...*labels.Matcher) error {

				for _, s := range series {
					chks := []tsdb_index.ChunkMeta{}
					for _, r := range s {
						chks = append(chks, tsdb_index.ChunkMeta{
							Checksum: r.ref.Checksum,
							KB:       r.KB,
							Entries:  r.Entries,
						})
					}

					if stop := fn(nil, s[0].ref.FingerprintModel(), chks); stop {
						return nil
					}
				}
				return nil
			},
		)
	}

	filtered := []*logproto.ChunkRef{
		// shard 0
		mkRef(1, 0),
		mkRef(1, 1),
		mkRef(1, 2),

		// shard 1
		mkRef(2, 10),
		mkRef(2, 20),
		mkRef(2, 30),

		// shard 2 split across multiple series
		mkRef(3, 10),
		mkRef(4, 10),
		mkRef(4, 20),

		// last shard contains leftovers + skip a few fps in between
		mkRef(7, 10),
	}

	series := [][]refWithSizingInfo{
		{
			// first series creates one shard since a shard can't contain partial series.
			// no chunks were filtered out
			sized(mkRef(1, 0), 100, 1),
			sized(mkRef(1, 1), 100, 1),
			sized(mkRef(1, 2), 100, 1),
		},
		{
			// second shard also contains one series, but this series has chunks filtered out.
			sized(mkRef(2, 0), 100, 1),  // filtered out
			sized(mkRef(2, 10), 100, 1), // included
			sized(mkRef(2, 11), 100, 1), // filtered out
			sized(mkRef(2, 20), 100, 1), // included
			sized(mkRef(2, 21), 100, 1), // filtered out
			sized(mkRef(2, 30), 100, 1), // included
			sized(mkRef(2, 31), 100, 1), // filtered out
		},

		// third shard contains multiple series.
		// combined they have 110kb, which is above the target of 100kb
		// but closer than leaving the second series out which would create
		// a shard with 50kb
		{
			// first series, 50kb
			sized(mkRef(3, 10), 50, 1), // 50kb
			sized(mkRef(3, 11), 50, 1), // 50kb, not included
		},
		{
			// second series
			sized(mkRef(4, 10), 30, 1), // 30kb
			sized(mkRef(4, 11), 30, 1), // 30kb, not included
			sized(mkRef(4, 20), 30, 1), // 30kb
		},

		// Fourth shard contains a single series with 25kb,
		// but iterates over non-included fp(s) before it
		{
			// register a series in the index which is not included in the filtered list
			sized(mkRef(6, 10), 100, 1), // not included
			sized(mkRef(6, 11), 100, 1), // not included
		},
		{
			// last shard contains leftovers
			sized(mkRef(7, 10), 25, 1),
			sized(mkRef(7, 11), 100, 1), // not included
		},
	}

	shards, grps, err := accumulateChunksToShards(
		context.Background(),
		"",
		fsImpl(series),
		&logproto.ShardsRequest{
			TargetBytesPerShard: 100 << 10,
		},
		chunk.NewPredicate(nil, nil), // we're not checking matcher injection here
		filtered,
	)

	expectedChks := [][]*logproto.ChunkRef{
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
			require.Equal(t, expectedChks[i][j], grps[i].Refs[j], "invalid chunk in grp %d at index %d", i, j)
		}
	}
	require.Equal(t, len(exp), len(shards))

}
