package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/weaveworks/cortex/pkg/chunk"
)

func TestAWSStorageClient(t *testing.T) {
	mockDB := newMockDynamoDB(0, 0)
	client := storageClient{
		DynamoDB:                mockDB,
		queryRequestFn:          mockDB.queryRequest,
		batchGetItemRequestFn:   mockDB.batchGetItemRequest,
		batchWriteItemRequestFn: mockDB.batchWriteItemRequest,
	}
	batch := client.NewWriteBatch()
	for i := 0; i < 30; i++ {
		batch.Add("table", fmt.Sprintf("hash%d", i), []byte(fmt.Sprintf("range%d", i)), nil)
	}
	mockDB.createTable("table")

	err := client.BatchWrite(context.Background(), batch)
	require.NoError(t, err)

	for i := 0; i < 30; i++ {
		entry := chunk.IndexQuery{
			TableName: "table",
			HashValue: fmt.Sprintf("hash%d", i),
		}
		var have []chunk.IndexEntry
		err := client.QueryPages(context.Background(), entry, func(read chunk.ReadBatch, lastPage bool) bool {
			for j := 0; j < read.Len(); j++ {
				have = append(have, chunk.IndexEntry{
					RangeValue: read.RangeValue(j),
				})
			}
			return !lastPage
		})
		require.NoError(t, err)
		require.Equal(t, []chunk.IndexEntry{
			{RangeValue: []byte(fmt.Sprintf("range%d", i))},
		}, have)
	}
}

func TestAWSStorageClientQueryPages(t *testing.T) {
	entries := []chunk.IndexEntry{
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("bar:1"),
			Value:      []byte("10"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("bar:2"),
			Value:      []byte("20"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("bar:3"),
			Value:      []byte("30"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("baz:1"),
			Value:      []byte("10"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("baz:2"),
			Value:      []byte("20"),
		},
		{
			TableName:  "table",
			HashValue:  "flip",
			RangeValue: []byte("bar:1"),
			Value:      []byte("abc"),
		},
		{
			TableName:  "table",
			HashValue:  "flip",
			RangeValue: []byte("bar:2"),
			Value:      []byte("abc"),
		},
		{
			TableName:  "table",
			HashValue:  "flip",
			RangeValue: []byte("bar:3"),
			Value:      []byte("abc"),
		},
	}

	tests := []struct {
		name           string
		query          chunk.IndexQuery
		provisionedErr int
		want           []chunk.IndexEntry
	}{
		{
			"check HashValue only",
			chunk.IndexQuery{
				TableName: "table",
				HashValue: "flip",
			},
			0,
			[]chunk.IndexEntry{entries[5], entries[6], entries[7]},
		},
		{
			"check RangeValueStart",
			chunk.IndexQuery{
				TableName:       "table",
				HashValue:       "foo",
				RangeValueStart: []byte("bar:2"),
			},
			0,
			[]chunk.IndexEntry{entries[1], entries[2], entries[3], entries[4]},
		},
		{
			"check RangeValuePrefix",
			chunk.IndexQuery{
				TableName:        "table",
				HashValue:        "foo",
				RangeValuePrefix: []byte("baz:"),
			},
			0,
			[]chunk.IndexEntry{entries[3], entries[4]},
		},
		{
			"check ValueEqual",
			chunk.IndexQuery{
				TableName:        "table",
				HashValue:        "foo",
				RangeValuePrefix: []byte("bar"),
				ValueEqual:       []byte("20"),
			},
			0,
			[]chunk.IndexEntry{entries[1]},
		},
		{
			"check retry logic",
			chunk.IndexQuery{
				TableName:        "table",
				HashValue:        "foo",
				RangeValuePrefix: []byte("bar"),
				ValueEqual:       []byte("20"),
			},
			2,
			[]chunk.IndexEntry{entries[1]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamoDB := newMockDynamoDB(0, tt.provisionedErr)
			client := storageClient{
				DynamoDB:                dynamoDB,
				queryRequestFn:          dynamoDB.queryRequest,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
			}

			batch := client.NewWriteBatch()
			for _, entry := range entries {
				batch.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
			}
			dynamoDB.createTable("table")

			err := client.BatchWrite(context.Background(), batch)
			require.NoError(t, err)

			var have []chunk.IndexEntry
			err = client.QueryPages(context.Background(), tt.query, func(read chunk.ReadBatch, lastPage bool) bool {
				for i := 0; i < read.Len(); i++ {
					have = append(have, chunk.IndexEntry{
						TableName:  tt.query.TableName,
						HashValue:  tt.query.HashValue,
						RangeValue: read.RangeValue(i),
						Value:      read.Value(i),
					})
				}
				return !lastPage
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, have)
		})
	}
}
