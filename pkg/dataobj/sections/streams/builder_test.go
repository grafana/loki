package streams_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func Test(t *testing.T) {
	type ent struct {
		Labels labels.Labels
		Time   time.Time
		Size   int64
	}

	tt := []ent{
		{labels.FromStrings("cluster", "test", "app", "foo"), time.Unix(10, 0), 10},
		{labels.FromStrings("cluster", "test", "app", "bar", "special", "yes"), time.Unix(100, 0), 20},
		{labels.FromStrings("cluster", "test", "app", "foo"), time.Unix(15, 0), 15},
		{labels.FromStrings("cluster", "test", "app", "foo"), time.Unix(9, 0), 5},
	}

	tracker := streams.NewBuilder(nil, 1024)
	for _, tc := range tt {
		tracker.Record(tc.Labels, tc.Time, tc.Size)
	}

	obj, closer, err := buildObject(tracker)
	require.NoError(t, err)
	defer closer.Close()

	expect := []streams.Stream{
		{
			ID:               1,
			Labels:           labels.FromStrings("cluster", "test", "app", "foo"),
			MinTimestamp:     time.Unix(9, 0),
			MaxTimestamp:     time.Unix(15, 0),
			Rows:             3,
			UncompressedSize: 30,
		},
		{
			ID:               2,
			Labels:           labels.FromStrings("cluster", "test", "app", "bar", "special", "yes"),
			MinTimestamp:     time.Unix(100, 0),
			MaxTimestamp:     time.Unix(100, 0),
			Rows:             1,
			UncompressedSize: 20,
		},
	}

	var actual []streams.Stream
	for result := range streams.Iter(context.Background(), obj) {
		stream, err := result.Value()
		require.NoError(t, err)
		stream.Labels = copyLabels(stream.Labels)
		actual = append(actual, stream)
	}

	require.Equal(t, expect, actual)
}

func copyLabels(in labels.Labels) labels.Labels {
	builder := labels.NewScratchBuilder(in.Len())

	in.Range(func(l labels.Label) {
		builder.Add(strings.Clone(l.Name), strings.Clone(l.Value))
	})

	builder.Sort()
	return builder.Labels()
}

func buildObject(st *streams.Builder) (*dataobj.Object, io.Closer, error) {
	builder := dataobj.NewBuilder(nil)
	if err := builder.Append(st); err != nil {
		return nil, nil, err
	}
	return builder.Flush()
}
