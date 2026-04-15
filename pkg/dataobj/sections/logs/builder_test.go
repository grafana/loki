package logs_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func Test(t *testing.T) {
	records := []logs.Record{
		{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.New(labels.Label{Name: "cluster", Value: "test"}, labels.Label{Name: "app", Value: "foo"}),
			Line:      []byte("foo bar"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.EmptyLabels(),
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  labels.New(labels.Label{Name: "cluster", Value: "test"}, labels.Label{Name: "app", Value: "bar"}),
			Line:      []byte("goodbye world"),
		},
	}

	opts := logs.BuilderOptions{
		PageSizeHint:     1024,
		BufferSize:       256,
		StripeMergeLimit: 2,
		SortOrder:        logs.SortStreamASC,
	}

	tracker := logs.NewBuilder(nil, opts)
	for _, record := range records {
		tracker.Append(record)
	}

	obj, closer, err := buildObject(tracker)
	require.NoError(t, err)
	defer closer.Close()

	// The order of records should be sorted by timestamp DESC then stream ID, and all
	// metadata should be sorted by key then value.
	expect := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.EmptyLabels(),
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  labels.New(labels.Label{Name: "app", Value: "bar"}, labels.Label{Name: "cluster", Value: "test"}),
			Line:      []byte("goodbye world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.New(labels.Label{Name: "app", Value: "foo"}, labels.Label{Name: "cluster", Value: "test"}),
			Line:      []byte("foo bar"),
		},
	}

	i := 0
	for result := range logs.Iter(context.Background(), obj) {
		record, err := result.Value()
		require.NoError(t, err)
		require.Equal(t, expect[i], record)
		i++
	}
}

func TestAppendOrdered(t *testing.T) {
	records := []logs.Record{
		{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.New(labels.Label{Name: "cluster", Value: "test"}, labels.Label{Name: "app", Value: "foo"}),
			Line:      []byte("foo bar"),
		},
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.EmptyLabels(),
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  labels.New(labels.Label{Name: "cluster", Value: "test"}, labels.Label{Name: "app", Value: "bar"}),
			Line:      []byte("goodbye world"),
		},
	}

	opts := logs.BuilderOptions{
		PageSizeHint:     1024,
		BufferSize:       256,
		StripeMergeLimit: 2,
		AppendStrategy:   logs.AppendOrdered,
		SortOrder:        logs.SortStreamASC,
	}

	tracker := logs.NewBuilder(nil, opts)
	for _, record := range records {
		tracker.Append(record)
	}

	obj, closer, err := buildObject(tracker)
	require.NoError(t, err)
	defer closer.Close()

	expect := []logs.Record{
		{
			StreamID:  1,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.EmptyLabels(),
			Line:      []byte("hello world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(100, 0),
			Metadata:  labels.New(labels.Label{Name: "app", Value: "bar"}, labels.Label{Name: "cluster", Value: "test"}),
			Line:      []byte("goodbye world"),
		},
		{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Metadata:  labels.New(labels.Label{Name: "app", Value: "foo"}, labels.Label{Name: "cluster", Value: "test"}),
			Line:      []byte("foo bar"),
		},
	}

	i := 0
	for result := range logs.Iter(context.Background(), obj) {
		record, err := result.Value()
		require.NoError(t, err)
		require.Equal(t, expect[i], record)
		i++
	}
	require.Equal(t, len(expect), i)
}

func TestAppendStrategies_Equivalent(t *testing.T) {
	records := []logs.Record{
		{StreamID: 3, Timestamp: time.Unix(50, 0), Metadata: labels.EmptyLabels(), Line: []byte("line-c")},
		{StreamID: 1, Timestamp: time.Unix(30, 0), Metadata: labels.EmptyLabels(), Line: []byte("line-a")},
		{StreamID: 2, Timestamp: time.Unix(10, 0), Metadata: labels.EmptyLabels(), Line: []byte("line-b1")},
		{StreamID: 1, Timestamp: time.Unix(20, 0), Metadata: labels.EmptyLabels(), Line: []byte("line-a2")},
		{StreamID: 2, Timestamp: time.Unix(40, 0), Metadata: labels.EmptyLabels(), Line: []byte("line-b2")},
		{StreamID: 3, Timestamp: time.Unix(60, 0), Metadata: labels.EmptyLabels(), Line: []byte("line-c2")},
	}

	for _, sortOrder := range []struct {
		name  string
		order logs.SortOrder
	}{
		{"SortStreamASC", logs.SortStreamASC},
		{"SortTimestampDESC", logs.SortTimestampDESC},
	} {
		t.Run(sortOrder.name, func(t *testing.T) {
			buildWithStrategy := func(strategy logs.AppendStrategy) []logs.Record {
				opts := logs.BuilderOptions{
					PageSizeHint:     1024,
					BufferSize:       64,
					StripeMergeLimit: 2,
					AppendStrategy:   strategy,
					SortOrder:        sortOrder.order,
				}
				builder := logs.NewBuilder(nil, opts)
				for _, r := range records {
					builder.Append(r)
				}
				obj, closer, err := buildObject(builder)
				require.NoError(t, err)
				defer closer.Close()

				var got []logs.Record
				for result := range logs.Iter(context.Background(), obj) {
					record, err := result.Value()
					require.NoError(t, err)
					got = append(got, record)
				}
				return got
			}

			unordered := buildWithStrategy(logs.AppendUnordered)
			ordered := buildWithStrategy(logs.AppendOrdered)

			require.Equal(t, unordered, ordered)
		})
	}
}

func TestAppendOrdered_ManyRecords(t *testing.T) {
	var records []logs.Record
	for i := range 500 {
		records = append(records, logs.Record{
			StreamID:  int64(i%5 + 1),
			Timestamp: time.Unix(int64(500-i), 0),
			Metadata:  labels.EmptyLabels(),
			Line:      []byte(fmt.Sprintf("line-%04d", i)),
		})
	}

	opts := logs.BuilderOptions{
		PageSizeHint:     4096,
		BufferSize:       1024,
		StripeMergeLimit: 2,
		AppendStrategy:   logs.AppendOrdered,
		SortOrder:        logs.SortStreamASC,
	}

	builder := logs.NewBuilder(nil, opts)
	for _, r := range records {
		builder.Append(r)
	}

	obj, closer, err := buildObject(builder)
	require.NoError(t, err)
	defer closer.Close()

	var got []logs.Record
	for result := range logs.Iter(context.Background(), obj) {
		record, err := result.Value()
		require.NoError(t, err)
		got = append(got, record)
	}

	require.Len(t, got, 500)

	// Verify sort order: streamID ASC, then timestamp DESC within each stream.
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if prev.StreamID == cur.StreamID {
			require.GreaterOrEqual(t, prev.Timestamp, cur.Timestamp,
				"timestamps not DESC within stream %d at index %d", cur.StreamID, i)
		} else {
			require.Less(t, prev.StreamID, cur.StreamID,
				"stream IDs not ASC at index %d", i)
		}
	}
}

func TestLogsBuilder_10000Columns(t *testing.T) {
	records := getRecords()

	opts := logs.BuilderOptions{
		PageSizeHint:     2 * 1024 * 1024,
		BufferSize:       256,
		StripeMergeLimit: 2,
		SortOrder:        logs.SortStreamASC,
	}

	dataobj := dataobj.NewBuilder(scratch.NewMemory())

	builder := logs.NewBuilder(nil, opts)
	for _, record := range records {
		builder.Append(record)
	}

	err := dataobj.Append(builder)
	require.NoError(t, err)

	_, closer, err := dataobj.Flush()
	require.NoError(t, err)
	require.NoError(t, closer.Close())
}

func BenchmarkLogsBuilder_10000Columns(b *testing.B) {
	records := getRecords()

	opts := logs.BuilderOptions{
		PageSizeHint:     2 * 1024 * 1024,
		BufferSize:       256 * 1024,
		StripeMergeLimit: 2,
		SortOrder:        logs.SortStreamASC,
	}

	b.ReportAllocs()
	for b.Loop() {
		dataobj := dataobj.NewBuilder(scratch.NewMemory())

		builder := logs.NewBuilder(nil, opts)
		for _, record := range records {
			builder.Append(record)
		}

		err := dataobj.Append(builder)
		require.NoError(b, err)

		_, closer, err := dataobj.Flush()
		require.NoError(b, err)
		require.NoError(b, closer.Close())
	}
}

func buildObject(lt *logs.Builder) (*dataobj.Object, io.Closer, error) {
	builder := dataobj.NewBuilder(nil)
	if err := builder.Append(lt); err != nil {
		return nil, nil, err
	}
	return builder.Flush()
}

func getRecords() []logs.Record {
	// Make a dataset of 100 records, each with 100 unique metadata labels, each with a single 256 byte value.
	var records []logs.Record
	for i := range 100 {
		megaRecord := logs.Record{
			StreamID:  2,
			Timestamp: time.Unix(10, 0),
			Line:      []byte("foo bar"),
		}
		lbb := labels.NewScratchBuilder(100)
		for j := range 100 {
			lbb.Add(fmt.Sprintf("key%d-%d", i, j), fmt.Sprintf("value%d-%d-%s", i, j, strings.Repeat("A", 256)))
		}
		megaRecord.Metadata = lbb.Labels()
		records = append(records, megaRecord)
	}
	return records
}
