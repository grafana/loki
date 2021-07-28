package indexgateway

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	util_math "github.com/cortexproject/cortex/pkg/util/math"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
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

func (r *mockBatch) Iterator() chunk.ReadBatchIterator {
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
	callback func(resp *indexgatewaypb.QueryIndexResponse)
}

func (m *mockQueryIndexServer) Send(resp *indexgatewaypb.QueryIndexResponse) error {
	m.callback(resp)
	return nil
}

func TestGateway_sendBatch(t *testing.T) {
	var expectedQueryKey string
	type batchRange struct {
		start, end int
	}
	var expectedRanges []batchRange

	// the response should have index entries between start and end from batchRange at index 0 of expectedRanges
	var server indexgatewaypb.IndexGateway_QueryIndexServer = &mockQueryIndexServer{
		callback: func(resp *indexgatewaypb.QueryIndexResponse) {
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

	gateway := gateway{}
	responseSizes := []int{0, 99, maxIndexEntriesPerResponse, 2 * maxIndexEntriesPerResponse, 5*maxIndexEntriesPerResponse - 1}
	for i, responseSize := range responseSizes {
		query := chunk.IndexQuery{
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

		err := gateway.sendBatch(server, query, &mockBatch{responseSize})
		require.NoError(t, err)

		// verify that we actually got responses back by checking if expectedRanges got cleared.
		require.Len(t, expectedRanges, 0)
	}
}
