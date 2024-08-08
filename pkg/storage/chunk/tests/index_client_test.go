package tests

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

var ctx = user.InjectOrgID(context.Background(), "1")

func TestIndexBasic(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, client index.Client, _ client.Client) {
		// Write out 30 entries, into different hash and range values.
		batch := client.NewWriteBatch()
		for i := 0; i < 30; i++ {
			batch.Add(tableName, fmt.Sprintf("hash%d", i), []byte(fmt.Sprintf("range%d", i)), nil)
		}
		err := client.BatchWrite(ctx, batch)
		require.NoError(t, err)

		// Make sure we get back the correct entries by hash value.
		for i := 0; i < 30; i++ {
			entries := []index.Query{
				{
					TableName: tableName,
					HashValue: fmt.Sprintf("hash%d", i),
				},
			}
			var have []index.Entry
			err := client.QueryPages(ctx, entries, func(_ index.Query, read index.ReadBatchResult) bool {
				iter := read.Iterator()
				for iter.Next() {
					have = append(have, index.Entry{
						RangeValue: iter.RangeValue(),
					})
				}
				return true
			})
			require.NoError(t, err)
			require.Equal(t, []index.Entry{
				{RangeValue: []byte(fmt.Sprintf("range%d", i))},
			}, have)
		}
	})
}

var entries = []index.Entry{
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
	forAllFixtures(t, func(t *testing.T, client index.Client, _ client.Client) {
		batch := client.NewWriteBatch()
		for _, entry := range entries {
			batch.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
		}

		err := client.BatchWrite(ctx, batch)
		require.NoError(t, err)

		tests := []struct {
			name   string
			query  index.Query
			repeat bool
			want   []index.Entry
		}{
			{
				"check HashValue only",
				index.Query{
					TableName: tableName,
					HashValue: "flip",
				},
				false,
				[]index.Entry{entries[5], entries[6], entries[7]},
			},
			{
				"check RangeValueStart",
				index.Query{
					TableName:       tableName,
					HashValue:       "foo",
					RangeValueStart: []byte("bar:2"),
				},
				false,
				[]index.Entry{entries[1], entries[2], entries[3], entries[4]},
			},
			{
				"check RangeValuePrefix",
				index.Query{
					TableName:        tableName,
					HashValue:        "foo",
					RangeValuePrefix: []byte("baz:"),
				},
				false,
				[]index.Entry{entries[3], entries[4]},
			},
			{
				"check ValueEqual",
				index.Query{
					TableName:        tableName,
					HashValue:        "foo",
					RangeValuePrefix: []byte("bar"),
					ValueEqual:       []byte("20"),
				},
				false,
				[]index.Entry{entries[1]},
			},
			{
				"check retry logic",
				index.Query{
					TableName:        tableName,
					HashValue:        "foo",
					RangeValuePrefix: []byte("bar"),
					ValueEqual:       []byte("20"),
				},
				true,
				[]index.Entry{entries[1]},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				run := true
				for run {
					var have []index.Entry
					err = client.QueryPages(ctx, []index.Query{tt.query}, func(_ index.Query, read index.ReadBatchResult) bool {
						iter := read.Iterator()
						for iter.Next() {
							have = append(have, index.Entry{
								TableName:  tt.query.TableName,
								HashValue:  tt.query.HashValue,
								RangeValue: iter.RangeValue(),
								Value:      iter.Value(),
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

func TestCardinalityLimit(t *testing.T) {
	forAllFixtures(t, func(t *testing.T, client index.Client, _ client.Client) {
		limits, err := defaultLimits()
		require.NoError(t, err)

		client = index.NewCachingIndexClient(client, cache.NewMockCache(), time.Minute, limits, log.NewNopLogger(), false)
		batch := client.NewWriteBatch()
		for i := 0; i < 10; i++ {
			batch.Add(tableName, "bar", []byte(strconv.Itoa(i)), []byte(strconv.Itoa(i)))
		}
		err = client.BatchWrite(ctx, batch)
		require.NoError(t, err)

		var have int
		err = client.QueryPages(ctx, []index.Query{{
			TableName: tableName,
			HashValue: "bar",
		}}, func(_ index.Query, read index.ReadBatchResult) bool {
			iter := read.Iterator()
			for iter.Next() {
				have++
			}
			return true
		})
		require.Error(t, err, "cardinality limit exceeded for {}; 10 entries, more than limit of 5")
		require.Equal(t, 0, have)
	})
}
