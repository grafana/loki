package aggregation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// decodeBody returns the push request in the snappy compressed body and
// the size of its uncompressed protobuf.
func decodeBody(t *testing.T, body []byte) (logproto.PushRequest, int) {
	t.Helper()
	raw, err := snappy.Decode(nil, body)
	require.NoError(t, err)
	var req logproto.PushRequest
	require.NoError(t, proto.Unmarshal(raw, &req))
	return req, len(raw)
}

func newBatchTestPush(maxBatchSize int) *Push {
	return &Push{
		tenantID:     "test1",
		logger:       log.NewNopLogger(),
		quit:         make(chan struct{}),
		backoff:      &backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxRetries: 1},
		metrics:      NewMetrics(nil),
		maxBatchSize: maxBatchSize,
	}
}

func TestPush_buildBatches(t *testing.T) {
	lbls1 := labels.New(labels.Label{Name: "foo", Value: "bar"})
	lbls2 := labels.New(labels.Label{Name: "baz", Value: "qux"})
	metadata := []logproto.LabelAdapter{{Name: constants.LevelLabel, Value: "info"}}
	ts, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	require.NoError(t, err)

	t.Run("a small number of entries is one batch", func(t *testing.T) {
		p := newBatchTestPush(0)
		p.WriteEntry(ts, "this is a line 1", lbls1, metadata)
		p.WriteEntry(ts, "this is a line 2", lbls2, metadata)

		batches := p.buildBatches()
		require.Len(t, batches, 1)
		require.Equal(t, 2, batches[0].count)
		require.Len(t, batches[0].entries, 2)
	})

	t.Run("no entries is no batches", func(t *testing.T) {
		p := newBatchTestPush(0)
		require.Empty(t, p.buildBatches())
	})

	t.Run("the buffer is drained", func(t *testing.T) {
		p := newBatchTestPush(0)
		p.WriteEntry(ts, "this is a line 1", lbls1, metadata)
		require.Len(t, p.buildBatches(), 1)
		require.Empty(t, p.buildBatches())
	})

	t.Run("many entries are split below the maximum batch size without losing entries", func(t *testing.T) {
		const maxBatchSize = 1024
		p := newBatchTestPush(maxBatchSize)
		want := make(map[string][]string)
		for i := range 100 {
			line := fmt.Sprintf("this is a line %d", i)
			want[lbls1.String()] = append(want[lbls1.String()], line)
			p.WriteEntry(ts.Add(time.Duration(i)*time.Second), line, lbls1, metadata)
		}
		for i := range 100 {
			line := fmt.Sprintf("this is a line %d", i)
			want[lbls2.String()] = append(want[lbls2.String()], line)
			p.WriteEntry(ts.Add(time.Duration(i)*time.Second), line, lbls2, metadata)
		}

		batches := p.buildBatches()
		require.Greater(t, len(batches), 1)

		actual := make(map[string][]string)
		var count int
		for _, b := range batches {
			require.LessOrEqual(t, b.size, maxBatchSize)
			var size, entries int
			for stream, es := range b.entries {
				for _, e := range es {
					size += entrySize(stream, e)
					entries++
					actual[stream] = append(actual[stream], e.Line)
				}
			}
			require.Equal(t, b.size, size, "size is the sum of the size of the entries")
			require.Equal(t, b.count, entries)
			count += b.count
		}
		require.Equal(t, 200, count)
		// None are lost or repeated, and they are in order within each stream.
		require.Equal(t, want, actual)
	})

	t.Run("an entry larger than the maximum batch size is in a batch of its own", func(t *testing.T) {
		const maxBatchSize = 1024
		p := newBatchTestPush(maxBatchSize)
		p.WriteEntry(ts, "this is a line 1", lbls1, metadata)
		p.WriteEntry(ts, "this is a line 2 "+strings.Repeat("x", 4*maxBatchSize), lbls1, metadata)
		p.WriteEntry(ts, "this is a line 3", lbls1, metadata)

		batches := p.buildBatches()
		require.Len(t, batches, 3)
		for _, b := range batches {
			require.Equal(t, 1, b.count)
		}
		require.LessOrEqual(t, batches[0].size, maxBatchSize)
		require.Greater(t, batches[1].size, maxBatchSize)
		require.LessOrEqual(t, batches[2].size, maxBatchSize)
	})
}

func TestPush_encode(t *testing.T) {
	lbls1 := labels.New(labels.Label{Name: "foo", Value: "bar"})
	lbls2 := labels.New(labels.Label{Name: "baz", Value: "qux"})
	metadata := []logproto.LabelAdapter{{Name: constants.LevelLabel, Value: "info"}}
	ts, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	require.NoError(t, err)

	t.Run("a batch is a snappy compressed push request", func(t *testing.T) {
		p := newBatchTestPush(0)
		p.WriteEntry(ts, "this is a line 1", lbls1, metadata)
		p.WriteEntry(ts, "this is a line 2", lbls2, metadata)
		p.WriteEntry(ts, "this is a line 3", lbls1, metadata)

		batches := p.buildBatches()
		require.Len(t, batches, 1)
		body, err := p.encode(batches[0])
		require.NoError(t, err)

		req, _ := decodeBody(t, body)
		require.Len(t, req.Streams, 2)
		lines := make(map[string][]string)
		for _, stream := range req.Streams {
			for _, entry := range stream.Entries {
				lines[stream.Labels] = append(lines[stream.Labels], entry.Line)
			}
		}
		require.Equal(t, map[string][]string{
			lbls1.String(): {"this is a line 1", "this is a line 3"},
			lbls2.String(): {"this is a line 2"},
		}, lines)
	})

	t.Run("the uncompressed size of an encoded batch is below the maximum batch size", func(t *testing.T) {
		// The size of a batch is an estimate. It is only below the real size of
		// the request for the stream labels of aggregated metrics and patterns,
		// which are long enough to make up for the encoding overhead of an entry,
		// so use the labels of an aggregated metric here.
		const maxBatchSize = 1024
		p := newBatchTestPush(maxBatchSize)
		lbls1 := labels.New(labels.Label{Name: constants.AggregatedMetricLabel, Value: "foo"})
		lbls2 := labels.New(labels.Label{Name: constants.PatternLabel, Value: "bar"})
		for i := range 100 {
			p.WriteEntry(ts.Add(time.Duration(i)*time.Second), fmt.Sprintf("this is a line %d", i), lbls1, metadata)
		}
		for i := range 100 {
			p.WriteEntry(ts.Add(time.Duration(i)*time.Second), fmt.Sprintf("this is a line %d", i), lbls2, metadata)
		}

		batches := p.buildBatches()
		require.Greater(t, len(batches), 1)
		for _, b := range batches {
			body, err := p.encode(b)
			require.NoError(t, err)
			_, size := decodeBody(t, body)
			require.LessOrEqual(t, size, maxBatchSize)
		}
	})

	t.Run("a batch without streams with valid labels is nil", func(t *testing.T) {
		p := newBatchTestPush(0)
		b := batch{entries: make(map[string][]logproto.Entry)}
		b.add("not labels", logproto.Entry{Line: "this is a line 1"}, 10)
		body, err := p.encode(b)
		require.NoError(t, err)
		require.Nil(t, body)
	})
}

func TestEntrySize(t *testing.T) {
	t.Run("empty entry", func(t *testing.T) {
		labels := `{foo="bar"}`
		require.Equal(t, len(labels)+8, entrySize(labels, logproto.Entry{}))
	})

	t.Run("non-empty entry", func(t *testing.T) {
		labels := `{foo="bar"}`
		line := "this is a line"
		entry := logproto.Entry{
			Line:               line,
			StructuredMetadata: []logproto.LabelAdapter{{Name: "baz", Value: "qux"}},
		}
		expectedSize := len(labels) + len(line) + 8 + len("baz") + len("qux")
		require.Equal(t, expectedSize, entrySize(labels, entry))
	})
}
