package streams_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
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

	tracker := streams.New(nil, 1024)
	for _, tc := range tt {
		tracker.Record(tc.Labels, tc.Time, tc.Size)
	}

	buf, err := buildObject(tracker)
	require.NoError(t, err)

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

	dec := encoding.ReaderAtDecoder(bytes.NewReader(buf), int64(len(buf)))

	var actual []streams.Stream
	for result := range streams.Iter(context.Background(), dec) {
		stream, err := result.Value()
		require.NoError(t, err)
		stream.Labels = copyLabels(stream.Labels)
		stream.LbValueCaps = nil
		actual = append(actual, stream)
	}

	require.Equal(t, expect, actual)
}

func copyLabels(in labels.Labels) labels.Labels {
	lb := make(labels.Labels, len(in))
	for i, label := range in {
		lb[i] = labels.Label{
			Name:  strings.Clone(label.Name),
			Value: strings.Clone(label.Value),
		}
	}
	return lb
}

func buildObject(st *streams.Streams) ([]byte, error) {
	var buf bytes.Buffer
	enc := encoding.NewEncoder()
	if err := st.EncodeTo(enc); err != nil {
		return nil, err
	} else if err := enc.Flush(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
