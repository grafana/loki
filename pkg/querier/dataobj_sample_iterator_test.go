package querier

import (
	"errors"
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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			it := newDataObjSampleIterator(newTestLogReader(tc.records, nil), mustExtractor(t, tc.query))
			require.Equal(t, tc.want, drainSampleIterator(it))
			require.NoError(t, it.Err())
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
