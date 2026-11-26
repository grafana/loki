package dataobjread

import (
	"errors"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestSampleIterator(t *testing.T) {
	newIterator := func(t *testing.T, query string, reader recordReader) *SampleIterator {
		t.Helper()
		expr, err := syntax.ParseSampleExpr(query)
		require.NoError(t, err)
		extractor, err := expr.Extractor()
		require.NoError(t, err)
		return NewSampleIterator(reader, extractor)
	}

	t.Run("it emits one sample per line with the stream's streamHash", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{
			record(t, `{app="a"}`, 1, "one"),
			record(t, `{app="a"}`, 2, "two"),
		}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		var got []sampleRow
		for it.Next() {
			got = append(got, sampleRow{
				Labels:       it.Labels(),
				TimestampSec: it.At().Timestamp / 1e9,
				Value:        it.At().Value,
				StreamHash:   it.StreamHash(),
			})
		}
		require.NoError(t, it.Err())
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(`{app="a"}`)},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(`{app="a"}`)},
		}, got)
	})

	t.Run("a sample is never given a hash, because nothing deduplicates this band", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{record(t, `{app="a"}`, 1, "one")}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.True(t, it.Next())
		require.Zero(t, it.At().Hash)
	})

	t.Run("a line the pipeline drops yields no sample", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{
			record(t, `{app="a"}`, 1, "keep me"),
			record(t, `{app="a"}`, 2, "drop me"),
		}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"} |= "keep" [1m]))`, reader)

		require.True(t, it.Next())
		require.EqualValues(t, 1, it.At().Timestamp/1e9)
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})

	t.Run("two streams sharing a streamHash each get their own extractor", func(t *testing.T) {
		// A stream-hash collision is what the labels.Equal guard in the extractor cache exists
		// for, so force one: same hash, different labels.
		first := record(t, `{app="a"}`, 1, "one")
		second := record(t, `{app="b"}`, 2, "two")
		second.streamHash = first.streamHash

		reader := &fakeRecordReader{records: []LogRecord{first, second}}
		it := newIterator(t, `count_over_time({app=~".+"}[1m])`, reader)

		var gotLabels []string
		for it.Next() {
			gotLabels = append(gotLabels, it.Labels())
		}
		require.NoError(t, it.Err())
		require.Equal(t, []string{`{app="a"}`, `{app="b"}`}, gotLabels,
			"the second stream must not be labelled with the first stream's labels")
	})

	t.Run("records from interleaved streams are each labelled with their own stream", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{
			record(t, `{app="a"}`, 1, "one"),
			record(t, `{app="b"}`, 1, "two"),
			record(t, `{app="a"}`, 2, "three"),
			record(t, `{app="b"}`, 2, "four"),
		}}
		it := newIterator(t, `count_over_time({app=~".+"}[1m])`, reader)

		var gotLabels []string
		for it.Next() {
			gotLabels = append(gotLabels, it.Labels())
		}
		require.NoError(t, it.Err())
		require.Equal(t, []string{`{app="a"}`, `{app="b"}`, `{app="a"}`, `{app="b"}`}, gotLabels)
	})

	t.Run("structured metadata reaches the output labels", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{
			record(t, `{app="a"}`, 1, "one", `{level="error"}`),
		}}
		it := newIterator(t, `count_over_time({app="a"}[1m])`, reader)

		require.True(t, it.Next())
		require.Equal(t, `{app="a", level="error"}`, it.Labels())
	})

	t.Run("it reports nothing before the first Next", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{record(t, `{app="a"}`, 1, "one")}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.Zero(t, it.At())
		require.Empty(t, it.Labels())
		require.Zero(t, it.StreamHash())
	})

	t.Run("it reports nothing once exhausted", func(t *testing.T) {
		reader := &fakeRecordReader{records: []LogRecord{record(t, `{app="a"}`, 1, "one")}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.True(t, it.Next())
		require.False(t, it.Next())
		require.Zero(t, it.At())
		require.Empty(t, it.Labels())
		require.Zero(t, it.StreamHash())
	})

	t.Run("it forwards the reader's error", func(t *testing.T) {
		wantErr := errors.New("scan failed")
		reader := &fakeRecordReader{err: wantErr}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("Close closes the reader and forwards its error", func(t *testing.T) {
		wantErr := errors.New("close failed")
		reader := &fakeRecordReader{closeErr: wantErr}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.ErrorIs(t, it.Close(), wantErr)
		require.True(t, reader.closed)
	})
}

// fakeRecordReader replays a fixed set of records, so a test can drive the sample iterator with
// records the read path would not produce.
type fakeRecordReader struct {
	records []LogRecord
	pos     int
	err     error

	closed   bool
	closeErr error
}

func (r *fakeRecordReader) Next() bool {
	if r.pos >= len(r.records) {
		return false
	}
	r.pos++
	return true
}

func (r *fakeRecordReader) At() LogRecord { return r.records[r.pos-1] }

func (r *fakeRecordReader) Err() error { return r.err }

func (r *fakeRecordReader) Close() error {
	r.closed = true
	return r.closeErr
}

// record builds one decoded log line for the given stream. Both streamLabels and the optional
// metadata are LogQL label sets.
func record(t *testing.T, streamLabels string, timestampSec int64, line string, metadata ...string) LogRecord {
	t.Helper()
	require.LessOrEqual(t, len(metadata), 1, "a line carries one structured-metadata label set")

	parsedStreamLabels, err := syntax.ParseLabels(streamLabels)
	require.NoError(t, err)

	parsedMetadata := labels.EmptyLabels()
	if len(metadata) == 1 {
		parsedMetadata, err = syntax.ParseLabels(metadata[0])
		require.NoError(t, err)
	}

	return LogRecord{
		streamHash:   labels.StableHash(parsedStreamLabels),
		streamLabels: parsedStreamLabels,
		timestamp:    timestampSec * 1e9,
		line:         []byte(line),
		metadata:     parsedMetadata,
	}
}
