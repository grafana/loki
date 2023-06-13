package indexgateway

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/weaveworks/common/user"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_test "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
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
		expectedQueryKey = util.QueryKey(query)
		gateway.indexClients = []IndexClientWithRange{{
			IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
			TableRange: config.TableRange{
				Start: 0,
				End:   math.MaxInt64,
				PeriodConfig: &config.PeriodConfig{
					IndexTables: config.PeriodicTableConfig{Prefix: tableNamePrefix},
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
			require.Equal(t, util.QueryKey(index.Query{
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
				IndexTables: config.PeriodicTableConfig{Prefix: tableNamePrefix},
			},
		},
	}, {
		IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
		TableRange: config.TableRange{
			Start: 5,
			End:   10,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.PeriodicTableConfig{Prefix: tableNamePrefix},
			},
		},
	}, {
		IndexClient: &mockIndexClient{response: &mockBatch{size: responseSize}},
		TableRange: config.TableRange{
			Start: 15,
			End:   math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.PeriodicTableConfig{Prefix: tableNamePrefix},
			},
		},
	}}
	gateway, err := NewIndexGateway(Config{}, util_log.Logger, nil, nil, indexClients)
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

func TestLabelVolume(t *testing.T) {
	indexQuerier := newIngesterQuerierMock()
	indexQuerier.On("LabelVolume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&logproto.LabelVolumeResponse{Volumes: []logproto.LabelVolume{
		{Name: "bar", Value: "baz", Volume: 38},
	}}, nil)

	gateway, err := NewIndexGateway(Config{}, util_log.Logger, nil, indexQuerier, nil)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	vol, err := gateway.GetLabelVolume(ctx, &logproto.LabelVolumeRequest{Matchers: "{}"})
	require.NoError(t, err)

	require.Equal(t, &logproto.LabelVolumeResponse{Volumes: []logproto.LabelVolume{
		{Name: "bar", Value: "baz", Volume: 38},
	}}, vol)
}

type indexQuerierMock struct {
	IndexQuerier
	util_test.ExtendedMock
}

func newIngesterQuerierMock() *indexQuerierMock {
	return &indexQuerierMock{}
}

func (i *indexQuerierMock) LabelVolume(_ context.Context, userID string, from, through model.Time, _ int32, matchers ...*labels.Matcher) (*logproto.LabelVolumeResponse, error) {
	args := i.Called(userID, from, through, matchers)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*logproto.LabelVolumeResponse), args.Error(1)
}
