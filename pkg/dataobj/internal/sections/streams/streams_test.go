package streams_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
)

func Test(t *testing.T) {
	type ent struct {
		Labels labels.Labels
		Time   time.Time
	}

	tt := []ent{
		{labels.FromStrings("cluster", "test", "app", "foo"), time.Unix(10, 0).UTC()},
		{labels.FromStrings("cluster", "test", "app", "bar", "special", "yes"), time.Unix(100, 0).UTC()},
		{labels.FromStrings("cluster", "test", "app", "foo"), time.Unix(15, 0).UTC()},
		{labels.FromStrings("cluster", "test", "app", "foo"), time.Unix(9, 0).UTC()},
	}

	tracker := streams.New(nil, 1024)
	for _, tc := range tt {
		tracker.Record(tc.Labels, tc.Time)
	}

	buf, err := buildObject(tracker)
	require.NoError(t, err)

	expect := []streams.Stream{
		{
			ID:           1,
			Labels:       labels.FromStrings("cluster", "test", "app", "foo"),
			MinTimestamp: time.Unix(9, 0).UTC(),
			MaxTimestamp: time.Unix(15, 0).UTC(),
			Rows:         3,
		},
		{
			ID:           2,
			Labels:       labels.FromStrings("cluster", "test", "app", "bar", "special", "yes"),
			MinTimestamp: time.Unix(100, 0).UTC(),
			MaxTimestamp: time.Unix(100, 0).UTC(),
			Rows:         1,
		},
	}

	dec := encoding.ReadSeekerDecoder(bytes.NewReader(buf))

	var actual []streams.Stream
	for result := range streams.Iter(context.Background(), dec) {
		stream, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, stream)
	}

	require.Equal(t, expect, actual)
}

func buildObject(st *streams.Streams) ([]byte, error) {
	var buf bytes.Buffer
	enc := encoding.NewEncoder(&buf)
	if err := st.EncodeTo(enc); err != nil {
		return nil, err
	} else if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
