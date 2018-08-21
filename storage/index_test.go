package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk"
)

func TestIndexBasic(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, client chunk.StorageClient) {
		// Write out 30 entries, into different hash and range values.
		batch := client.NewWriteBatch()
		for i := 0; i < 30; i++ {
			batch.Add(tableName, fmt.Sprintf("hash%d", i), []byte(fmt.Sprintf("range%d", i)), nil)
		}
		err := client.BatchWrite(context.Background(), batch)
		require.NoError(t, err)

		// Make sure we get back the correct entries by hash value.
		for i := 0; i < 30; i++ {
			entry := chunk.IndexQuery{
				TableName: tableName,
				HashValue: fmt.Sprintf("hash%d", i),
			}
			var have []chunk.IndexEntry
			err := client.QueryPages(context.Background(), entry, func(read chunk.ReadBatch) bool {
				for j := 0; j < read.Len(); j++ {
					have = append(have, chunk.IndexEntry{
						RangeValue: read.RangeValue(j),
					})
				}
				return true
			})
			require.NoError(t, err)
			require.Equal(t, []chunk.IndexEntry{
				{RangeValue: []byte(fmt.Sprintf("range%d", i))},
			}, have)
		}
	})
}

var entries = []chunk.IndexEntry{
	{
		TableName:  tableName,
		HashValue:  "foo",
		RangeValue: []byte("bar:1"),
		Value:      []byte("10"),
	},
	{
		TableName:  tableName,
		HashValue:  "foo",
		RangeValue: []byte("bar:2"),
		Value:      []byte("20"),
	},
	{
		TableName:  tableName,
		HashValue:  "foo",
		RangeValue: []byte("bar:3"),
		Value:      []byte("30"),
	},
	{
		TableName:  tableName,
		HashValue:  "foo",
		RangeValue: []byte("baz:1"),
		Value:      []byte("10"),
	},
	{
		TableName:  tableName,
		HashValue:  "foo",
		RangeValue: []byte("baz:2"),
		Value:      []byte("20"),
	},
	{
		TableName:  tableName,
		HashValue:  "flip",
		RangeValue: []byte("bar:1"),
		Value:      []byte("abc"),
	},
	{
		TableName:  tableName,
		HashValue:  "flip",
		RangeValue: []byte("bar:2"),
		Value:      []byte("abc"),
	},
	{
		TableName:  tableName,
		HashValue:  "flip",
		RangeValue: []byte("bar:3"),
		Value:      []byte("abc"),
	},
}

func TestQueryPages(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, client chunk.StorageClient) {
		batch := client.NewWriteBatch()
		for _, entry := range entries {
			batch.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
		}

		err := client.BatchWrite(context.Background(), batch)
		require.NoError(t, err)

		tests := []struct {
			name   string
			query  chunk.IndexQuery
			repeat bool
			want   []chunk.IndexEntry
		}{
			{
				"check HashValue only",
				chunk.IndexQuery{
					TableName: tableName,
					HashValue: "flip",
				},
				false,
				[]chunk.IndexEntry{entries[5], entries[6], entries[7]},
			},
			{
				"check RangeValueStart",
				chunk.IndexQuery{
					TableName:       tableName,
					HashValue:       "foo",
					RangeValueStart: []byte("bar:2"),
				},
				false,
				[]chunk.IndexEntry{entries[1], entries[2], entries[3], entries[4]},
			},
			{
				"check RangeValuePrefix",
				chunk.IndexQuery{
					TableName:        tableName,
					HashValue:        "foo",
					RangeValuePrefix: []byte("baz:"),
				},
				false,
				[]chunk.IndexEntry{entries[3], entries[4]},
			},
			{
				"check ValueEqual",
				chunk.IndexQuery{
					TableName:        tableName,
					HashValue:        "foo",
					RangeValuePrefix: []byte("bar"),
					ValueEqual:       []byte("20"),
				},
				false,
				[]chunk.IndexEntry{entries[1]},
			},
			{
				"check retry logic",
				chunk.IndexQuery{
					TableName:        tableName,
					HashValue:        "foo",
					RangeValuePrefix: []byte("bar"),
					ValueEqual:       []byte("20"),
				},
				true,
				[]chunk.IndexEntry{entries[1]},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				run := true
				for run {
					var have []chunk.IndexEntry
					err = client.QueryPages(context.Background(), tt.query, func(read chunk.ReadBatch) bool {
						for i := 0; i < read.Len(); i++ {
							have = append(have, chunk.IndexEntry{
								TableName:  tt.query.TableName,
								HashValue:  tt.query.HashValue,
								RangeValue: read.RangeValue(i),
								Value:      read.Value(i),
							})
						}
						return true
					})
					require.NoError(t, err)
					require.Equal(t, tt.want, have)

					if tt.repeat {
						tt.repeat = false
					} else {
						run = false
					}
				}
			})
		}
	})
}
