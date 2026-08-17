package querier

import (
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func mustExtractor(t *testing.T, query string) syntax.SampleExtractor {
	t.Helper()
	expr, err := syntax.ParseSampleExpr(query)
	require.NoError(t, err)
	ex, err := expr.Extractor()
	require.NoError(t, err)
	return ex
}

func testLogRecord(fp uint64, lbls labels.Labels, ts int64, line string) dataObjLogRecord {
	return dataObjLogRecord{
		fingerprint:  fp,
		streamLabels: lbls,
		timestamp:    ts,
		line:         []byte(line),
	}
}

// newTestLogReader builds a dataObjLogReader that yields the given records as a single batch then
// reports err. See newTestLogReaderBatches for the batch-level seam.
func newTestLogReader(records []dataObjLogRecord, err error) *dataObjLogReader {
	var batches [][]dataObjLogRecord
	if len(records) > 0 {
		batches = [][]dataObjLogRecord{records}
	}
	return newTestLogReaderBatches(batches, err)
}

type emittedSample struct {
	streamHash uint64
	timestamp  int64
	labels     string
	value      float64
}

func drainSampleIterator(it *dataObjSampleIterator) []emittedSample {
	var out []emittedSample
	for it.Next() {
		s := it.At()
		out = append(out, emittedSample{
			streamHash: it.StreamHash(),
			timestamp:  s.Timestamp,
			labels:     it.Labels(),
			value:      s.Value,
		})
	}
	return out
}

func TestDataObjSampleIterator_Next(t *testing.T) {
	streamX := labels.FromStrings("app", "x")
	streamY := labels.FromStrings("app", "y")

	tests := map[string]struct {
		query   string
		records []dataObjLogRecord
		want    []emittedSample
	}{
		"no records": {
			query:   `count_over_time({app="x"}[1m])`,
			records: nil,
			want:    nil,
		},
		"single record, one sample": {
			query:   `count_over_time({app="x"}[1m])`,
			records: []dataObjLogRecord{testLogRecord(1, streamX, 100, "hello")},
			want: []emittedSample{
				{streamHash: 1, timestamp: 100, labels: streamX.String(), value: 1},
			},
		},
		"multiple records, same stream": {
			query: `count_over_time({app="x"}[1m])`,
			records: []dataObjLogRecord{
				testLogRecord(1, streamX, 100, "a"),
				testLogRecord(1, streamX, 200, "b"),
			},
			want: []emittedSample{
				{streamHash: 1, timestamp: 100, labels: streamX.String(), value: 1},
				{streamHash: 1, timestamp: 200, labels: streamX.String(), value: 1},
			},
		},
		"multiple streams interleaved": {
			query: `count_over_time({app=~".+"}[1m])`,
			records: []dataObjLogRecord{
				testLogRecord(1, streamX, 100, "a"),
				testLogRecord(2, streamY, 150, "b"),
				testLogRecord(1, streamX, 200, "c"),
			},
			want: []emittedSample{
				{streamHash: 1, timestamp: 100, labels: streamX.String(), value: 1},
				{streamHash: 2, timestamp: 150, labels: streamY.String(), value: 1},
				{streamHash: 1, timestamp: 200, labels: streamX.String(), value: 1},
			},
		},
		"record dropped by line filter is skipped": {
			query: `count_over_time({app="x"} |= "keep" [1m])`,
			records: []dataObjLogRecord{
				testLogRecord(1, streamX, 100, "keep this"),
				testLogRecord(1, streamX, 200, "drop this"),
				testLogRecord(1, streamX, 300, "keep that"),
			},
			want: []emittedSample{
				{streamHash: 1, timestamp: 100, labels: streamX.String(), value: 1},
				{streamHash: 1, timestamp: 300, labels: streamX.String(), value: 1},
			},
		},
		"all records dropped by line filter": {
			query: `count_over_time({app="x"} |= "keep" [1m])`,
			records: []dataObjLogRecord{
				testLogRecord(1, streamX, 100, "nope"),
				testLogRecord(1, streamX, 200, "still nope"),
			},
			want: nil,
		},
		// Two distinct streams sharing a fingerprint (a StableHash collision) must each report their own
		// labels: the last-stream cache is keyed on the fingerprint but verified by labels, so the second
		// record must miss the cache and rebuild rather than reuse the first stream's extractor.
		"fingerprint collision keeps streams distinct": {
			query: `count_over_time({app=~".+"}[1m])`,
			records: []dataObjLogRecord{
				testLogRecord(1, streamX, 100, "a"),
				testLogRecord(1, streamY, 200, "b"), // same fingerprint, different labels
				testLogRecord(1, streamX, 300, "c"),
			},
			want: []emittedSample{
				{streamHash: 1, timestamp: 100, labels: streamX.String(), value: 1},
				{streamHash: 1, timestamp: 200, labels: streamY.String(), value: 1},
				{streamHash: 1, timestamp: 300, labels: streamX.String(), value: 1},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			it := newDataObjSampleIterator(newTestLogReader(tc.records, nil), mustExtractor(t, tc.query))
			require.Equal(t, tc.want, drainSampleIterator(it))
			require.NoError(t, it.Err())
		})
	}
}

// BenchmarkDataObjSampleIterator isolates the per-line iterator + extractor cost (a synthetic reader, no
// object-storage decode), across two axes: normal vs the constant-label fast path (C4), and the record
// order the iterator sees. Each batch is one stream's rows (the logs section is stream-sorted), so the
// realistic order is "clustered" (a stream's batches contiguous) or "batch-interleaved" (a stream's
// batches split apart because the concurrently-scanned sections interleave). It reports ns/op and
// allocs/op per drained sample set.
func BenchmarkDataObjSampleIterator(b *testing.B) {
	const (
		numStreams = 128
		perStream  = 3000 // > batchSize, so a stream spans several batches (exposes the last-stream cache)
		batchSize  = 1024
	)

	streams := make([]labels.Labels, numStreams)
	for i := range streams {
		streams[i] = labels.FromStrings("app", "svc", "pod", fmt.Sprintf("pod-%04d", i))
	}

	// perStreamBatches[i] is stream i's rows chunked into batches. Each batch holds one stream's rows.
	perStreamBatches := make([][][]dataObjLogRecord, numStreams)
	for i := range streams {
		for start := 0; start < perStream; start += batchSize {
			end := min(start+batchSize, perStream)
			batch := make([]dataObjLogRecord, 0, end-start)
			for j := start; j < end; j++ {
				batch = append(batch, testLogRecord(uint64(i), streams[i], int64(j), "line"))
			}
			perStreamBatches[i] = append(perStreamBatches[i], batch)
		}
	}

	buildBatches := func(interleaved bool) [][]dataObjLogRecord {
		var batches [][]dataObjLogRecord
		if interleaved {
			for round := 0; ; round++ { // round-robin batches across streams, mimicking concurrent sections
				any := false
				for i := range streams {
					if round < len(perStreamBatches[i]) {
						batches = append(batches, perStreamBatches[i][round])
						any = true
					}
				}
				if !any {
					break
				}
			}
		} else {
			for i := range streams { // each stream's batches contiguous
				batches = append(batches, perStreamBatches[i]...)
			}
		}
		return batches
	}

	newExtractor := func(b *testing.B, constant bool) syntax.SampleExtractor {
		// A grouping that resolves to stream labels ("app", "pod") is constant per stream, so the
		// extractor builds the output labels once; a bare count_over_time keeps the full label set and
		// rebuilds it per line.
		query := `count_over_time({app="svc"}[1h])`
		if constant {
			query = `sum by (app, pod) (count_over_time({app="svc"}[1h]))`
		}
		expr, err := syntax.ParseSampleExpr(query)
		require.NoError(b, err)
		ex, err := expr.Extractor()
		require.NoError(b, err)
		return ex
	}

	for _, tc := range []struct {
		name        string
		interleaved bool
		constant    bool
	}{
		{"clustered/normal", false, false},
		{"clustered/constant", false, true},
		{"batch-interleaved/normal", true, false},
		{"batch-interleaved/constant", true, true},
	} {
		batches := buildBatches(tc.interleaved)
		b.Run(tc.name, func(b *testing.B) {
			ex := newExtractor(b, tc.constant)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				it := newDataObjSampleIterator(newTestLogReaderBatches(batches, nil), ex)
				n := 0
				for it.Next() {
					_ = it.At()
					n++
				}
				if n != numStreams*perStream {
					b.Fatalf("drained %d samples, want %d", n, numStreams*perStream)
				}
			}
		})
	}
}

func TestDataObjSampleIterator_At(t *testing.T) {
	streamX := labels.FromStrings("app", "x")

	t.Run("zero value before Next", func(t *testing.T) {
		it := newDataObjSampleIterator(newTestLogReader(nil, nil), mustExtractor(t, `count_over_time({app="x"}[1m])`))
		require.Equal(t, logproto.Sample{}, it.At())
	})

	t.Run("count sample carries record timestamp and unit value", func(t *testing.T) {
		it := newDataObjSampleIterator(
			newTestLogReader([]dataObjLogRecord{testLogRecord(1, streamX, 123, "line")}, nil),
			mustExtractor(t, `count_over_time({app="x"}[1m])`),
		)
		require.True(t, it.Next())
		require.Equal(t, logproto.Sample{Timestamp: 123, Value: 1}, it.At())
		// At does not advance: repeated calls return the same sample.
		require.Equal(t, logproto.Sample{Timestamp: 123, Value: 1}, it.At())
	})

	t.Run("bytes sample value is the line length", func(t *testing.T) {
		it := newDataObjSampleIterator(
			newTestLogReader([]dataObjLogRecord{testLogRecord(1, streamX, 7, "hello")}, nil),
			mustExtractor(t, `bytes_over_time({app="x"}[1m])`),
		)
		require.True(t, it.Next())
		require.Equal(t, logproto.Sample{Timestamp: 7, Value: 5}, it.At())
	})
}

func TestDataObjSampleIterator_Labels(t *testing.T) {
	streamX := labels.FromStrings("app", "x")
	streamY := labels.FromStrings("app", "y")

	t.Run("empty before Next", func(t *testing.T) {
		it := newDataObjSampleIterator(newTestLogReader(nil, nil), mustExtractor(t, `count_over_time({app="x"}[1m])`))
		require.Equal(t, "", it.Labels())
	})

	t.Run("renders the current record stream labels", func(t *testing.T) {
		records := []dataObjLogRecord{
			testLogRecord(1, streamX, 100, "a"),
			testLogRecord(2, streamY, 200, "b"),
		}
		it := newDataObjSampleIterator(newTestLogReader(records, nil), mustExtractor(t, `count_over_time({app=~".+"}[1m])`))

		require.True(t, it.Next())
		require.Equal(t, streamX.String(), it.Labels())
		require.True(t, it.Next())
		require.Equal(t, streamY.String(), it.Labels())
	})
}

func TestDataObjSampleIterator_StreamHash(t *testing.T) {
	streamX := labels.FromStrings("app", "x")

	t.Run("zero before Next", func(t *testing.T) {
		it := newDataObjSampleIterator(newTestLogReader(nil, nil), mustExtractor(t, `count_over_time({app="x"}[1m])`))
		require.Equal(t, uint64(0), it.StreamHash())
	})

	t.Run("tracks the current record fingerprint", func(t *testing.T) {
		records := []dataObjLogRecord{
			testLogRecord(42, streamX, 100, "a"),
			testLogRecord(7, streamX, 200, "b"),
		}
		it := newDataObjSampleIterator(newTestLogReader(records, nil), mustExtractor(t, `count_over_time({app="x"}[1m])`))

		require.True(t, it.Next())
		require.Equal(t, uint64(42), it.StreamHash())
		require.True(t, it.Next())
		require.Equal(t, uint64(7), it.StreamHash())
	})
}

func TestDataObjSampleIterator_Err(t *testing.T) {
	streamX := labels.FromStrings("app", "x")

	t.Run("nil when the reader reports no error", func(t *testing.T) {
		it := newDataObjSampleIterator(
			newTestLogReader([]dataObjLogRecord{testLogRecord(1, streamX, 1, "a")}, nil),
			mustExtractor(t, `count_over_time({app="x"}[1m])`),
		)
		drainSampleIterator(it)
		require.NoError(t, it.Err())
	})

	t.Run("stops without yielding buffered records once the reader errors", func(t *testing.T) {
		wantErr := errors.New("scan failed")
		it := newDataObjSampleIterator(
			newTestLogReader([]dataObjLogRecord{testLogRecord(1, streamX, 1, "a")}, wantErr),
			mustExtractor(t, `count_over_time({app="x"}[1m])`),
		)

		// The reader reports an error, so iteration stops without yielding the buffered record.
		require.Empty(t, drainSampleIterator(it))
		require.ErrorIs(t, it.Err(), wantErr)
	})
}

func TestDataObjSampleIterator_Close(t *testing.T) {
	t.Run("nil when the reader reports no error", func(t *testing.T) {
		it := newDataObjSampleIterator(newTestLogReader(nil, nil), mustExtractor(t, `count_over_time({app="x"}[1m])`))
		require.NoError(t, it.Close())
	})

	t.Run("propagates the reader error", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := newDataObjSampleIterator(newTestLogReader(nil, wantErr), mustExtractor(t, `count_over_time({app="x"}[1m])`))
		require.ErrorIs(t, it.Close(), wantErr)
	})
}
