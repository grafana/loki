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

func TestLogsBuilder_10000Columns(t *testing.T) {
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

func buildObject(lt *logs.Builder) (*dataobj.Object, io.Closer, error) {
	builder := dataobj.NewBuilder(nil)
	if err := builder.Append(lt); err != nil {
		return nil, nil, err
	}
	return builder.Flush()
}
