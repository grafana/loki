package dataobjread

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestSampleIterator(t *testing.T) {
	pool := newRowsBatchPool()

	newIterator := func(t *testing.T, query string, reader rowsBatchReader) *SampleIterator {
		t.Helper()
		expr, err := syntax.ParseSampleExpr(query)
		require.NoError(t, err)
		extractor, err := expr.Extractor()
		require.NoError(t, err)
		return NewSampleIterator(reader, extractor)
	}

	t.Run("it emits one sample per line with the stream's streamHash", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)}}
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
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.True(t, it.Next())
		require.Zero(t, it.At().Hash)
	})

	t.Run("a line the pipeline drops yields no sample", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "keep me"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "drop me"},
		)}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"} |= "keep" [1m]))`, reader)

		require.True(t, it.Next())
		require.EqualValues(t, 1, it.At().Timestamp/1e9)
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})

	t.Run("two streams sharing a streamHash each get their own extractor", func(t *testing.T) {
		// A stream-hash collision is what the labels.Equal guard in the extractor cache exists
		// for, so force one: same hash, different labels.
		hash := streamHashOf(`{app="a"}`)
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, streamHash: hash, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="b"}`, streamHash: hash, timestampSec: 2, line: "two"},
		)}}
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
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 1, line: "two"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "three"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 2, line: "four"},
		)}}
		it := newIterator(t, `count_over_time({app=~".+"}[1m])`, reader)

		var gotLabels []string
		for it.Next() {
			gotLabels = append(gotLabels, it.Labels())
		}
		require.NoError(t, it.Err())
		require.Equal(t, []string{`{app="a"}`, `{app="b"}`, `{app="a"}`, `{app="b"}`}, gotLabels)
	})

	t.Run("structured metadata reaches the output labels", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one", metadata: `{level="error"}`},
		)}}
		it := newIterator(t, `count_over_time({app="a"}[1m])`, reader)

		require.True(t, it.Next())
		require.Equal(t, `{app="a", level="error"}`, it.Labels())
	})

	t.Run("it reports nothing before the first Next", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.Zero(t, it.At())
		require.Empty(t, it.Labels())
		require.Zero(t, it.StreamHash())
	})

	t.Run("it reports nothing once exhausted", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})}}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.True(t, it.Next())
		require.False(t, it.Next())
		require.Zero(t, it.At())
		require.Empty(t, it.Labels())
		require.Zero(t, it.StreamHash())
	})

	t.Run("it forwards the reader's error", func(t *testing.T) {
		wantErr := errors.New("scan failed")
		reader := &fakeRowsBatchReader{err: wantErr}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("Close closes the reader and forwards its error", func(t *testing.T) {
		wantErr := errors.New("close failed")
		reader := &fakeRowsBatchReader{closeErr: wantErr}
		it := newIterator(t, `sum by (app) (count_over_time({app="a"}[1m]))`, reader)

		require.ErrorIs(t, it.Close(), wantErr)
		require.True(t, reader.closed)
	})

	t.Run("it emits the samples of every batch in the order of the batches", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{
			newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}, testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"}),
			newTestBatch(t, pool, testRow{streamLabels: `{app="b"}`, timestampSec: 3, line: "three"}),
		}}
		it := newIterator(t, `count_over_time({app=~".+"}[1m])`, reader)

		var got []string
		for it.Next() {
			got = append(got, fmt.Sprintf("%s@%d", it.Labels(), it.At().Timestamp/1e9))
		}
		require.NoError(t, it.Err())
		require.Equal(t, []string{`{app="a"}@1`, `{app="a"}@2`, `{app="b"}@3`}, got)
	})

	t.Run("a row with an empty line yields a sample", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: ""},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)
		it := newIterator(t, `sum(bytes_over_time({app="a"}[1m]))`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		var got []float64
		for it.Next() {
			got = append(got, it.At().Value)
		}
		require.Equal(t, []float64{0, 3}, got)
	})

	t.Run("a batch without a message column yields a sample for each row", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)
		batch.records.Message = nil
		batch.hasLine = false
		it := newIterator(t, `count_over_time({app="a"}[1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		var got []float64
		for it.Next() {
			got = append(got, it.At().Value)
		}
		require.Equal(t, []float64{1, 1}, got)
	})

	t.Run("a line that the batch does not carry reaches the pipeline as no line", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "twelve bytes"})
		batch.hasLine = false
		it := newIterator(t, `sum(bytes_over_time({app="a"}[1m]))`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.True(t, it.Next())
		require.Zero(t, it.At().Value)
	})

	t.Run("a line reaches the pipeline when the batch carries it", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "twelve bytes"})
		it := newIterator(t, `sum(bytes_over_time({app="a"}[1m]))`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.True(t, it.Next())
		require.EqualValues(t, len("twelve bytes"), it.At().Value)
	})

	t.Run("a stream that continues in the next batch keeps its labels and its stream hash", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{
			newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}),
			newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"}),
		}}
		it := newIterator(t, `count_over_time({app=~".+"}[1m])`, reader)

		var got []sampleRow
		for it.Next() {
			got = append(got, sampleRow{Labels: it.Labels(), TimestampSec: it.At().Timestamp / 1e9, Value: it.At().Value, StreamHash: it.StreamHash()})
		}
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(`{app="a"}`)},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(`{app="a"}`)},
		}, got)
	})

	t.Run("each row gets its own structured metadata, and a row without a value gets none", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one", metadata: `{level="error"}`},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 3, line: "three", metadata: `{level="info"}`},
		)}}
		it := newIterator(t, `count_over_time({app="a"}[1m])`, reader)

		var got []string
		for it.Next() {
			got = append(got, it.Labels())
		}
		require.Equal(t, []string{`{app="a", level="error"}`, `{app="a"}`, `{app="a", level="info"}`}, got)
	})

	t.Run("it releases a batch after reading its last row, and not before", func(t *testing.T) {
		first := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)
		second := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 3, line: "three"})
		it := newIterator(t, `count_over_time({app="a"}[1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{first, second}})

		require.True(t, it.Next())
		require.True(t, it.Next())
		require.False(t, first.pooled, "the iterator has returned the last row of the batch, but not moved past it")

		require.True(t, it.Next())
		require.True(t, first.pooled)
		require.False(t, second.pooled)

		require.False(t, it.Next())
		require.True(t, second.pooled)
	})

	t.Run("it releases a batch in which the pipeline drops every row", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "drop me"})
		it := newIterator(t, `count_over_time({app="a"} |= "keep" [1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.False(t, it.Next())
		require.True(t, batch.pooled)
	})

	t.Run("it releases a batch before it asks the reader for the next one", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{
			newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"}),
			newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"}),
		}}
		it := newIterator(t, `count_over_time({app="a"}[1m])`, reader)

		for it.Next() {
		}

		require.Equal(t, []bool{true, true, true}, reader.previousReleasedOnAsk)
	})

	t.Run("Close releases the batch that the iterator is reading", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "two"},
		)
		it := newIterator(t, `count_over_time({app="a"}[1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.True(t, it.Next())
		require.False(t, batch.pooled)

		require.NoError(t, it.Close())
		require.True(t, batch.pooled)
	})

	t.Run("it reports nothing after Close", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		it := newIterator(t, `count_over_time({app="a"}[1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.True(t, it.Next())
		require.NoError(t, it.Close())
		require.Zero(t, it.At())
		require.Empty(t, it.Labels())
		require.Zero(t, it.StreamHash())
	})

	t.Run("Next returns false after Close", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		it := newIterator(t, `count_over_time({app="a"}[1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.NoError(t, it.Close())
		require.False(t, it.Next())
		require.Zero(t, it.At())
	})

	t.Run("a second Close closes the reader again and does not release a batch twice", func(t *testing.T) {
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"})
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{batch}}
		it := newIterator(t, `count_over_time({app="a"}[1m])`, reader)

		require.True(t, it.Next())
		require.NoError(t, it.Close())
		require.NoError(t, it.Close())
		require.Equal(t, 2, reader.closes)
	})

	t.Run("a real read with a parser and a filter yields the samples of the rows that pass, at every batch size and section layout", func(t *testing.T) {
		const (
			streamCount      = 3
			entriesPerStream = 40
		)
		levels := []string{"info", "error"}
		statuses := []string{"200", "500", "404"}

		for _, tc := range []struct {
			name  string
			query string
			line  func(level, status string) string
		}{
			{
				name:  "a logfmt pipeline yields the samples of the rows that pass",
				query: `sum by (app, level, pod) (count_over_time({app=~".+"} | logfmt | status="500" [1m]))`,
				line:  func(level, status string) string { return fmt.Sprintf("level=%s status=%s msg=request", level, status) },
			},
			{
				name:  "a json pipeline yields the samples of the rows that pass",
				query: `sum by (app, level, pod) (count_over_time({app=~".+"} | json | status="500" [1m]))`,
				line:  func(level, status string) string { return fmt.Sprintf(`{"level":%q,"status":%q}`, level, status) },
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var (
					streams []logproto.Stream
					want    []sampleRow
				)
				for s := range streamCount {
					streamLabels := fmt.Sprintf(`{app="app-%d"}`, s)
					stream := logproto.Stream{Labels: streamLabels}
					for i := range entriesPerStream {
						level, status := levels[i%len(levels)], statuses[i%len(statuses)]

						var metadata []string
						wantLabels := fmt.Sprintf(`{app="app-%d", level=%q}`, s, level)
						if i%4 == 0 {
							metadata = []string{"pod", "p1"}
							wantLabels = fmt.Sprintf(`{app="app-%d", level=%q, pod="p1"}`, s, level)
						}
						stream.Entries = append(stream.Entries, entry(t, i+1, tc.line(level, status), metadata...))

						if status == "500" {
							want = append(want, sampleRow{Labels: wantLabels, TimestampSec: at(i + 1).Unix(), Value: 1, StreamHash: streamHashOf(streamLabels)})
						}
					}
					streams = append(streams, stream)
				}
				require.NotEmpty(t, want)

				for _, layout := range []struct {
					name          string
					opts          []objtest.Option
					multipleTasks bool
				}{
					{name: "in sections that each hold one stream", opts: []objtest.Option{objtest.WithTargetSectionSize(1 << 10)}, multipleTasks: true},
					{name: "in one section that holds every stream", opts: nil, multipleTasks: false},
				} {
					fixture := planReaderFixture(t, tc.query, newObjectsFixtureWithOptions(t, layout.opts, streams...))
					require.Equal(t, layout.multipleTasks, len(fixture.tasks) > 1, layout.name)

					for _, batchSize := range []int{1, 3, DefaultReadBatchSize} {
						reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, NewMetrics(nil))
						it := NewSampleIterator(reader, sampleExtractor(t, tc.query))

						var got []sampleRow
						for it.Next() {
							got = append(got, sampleRow{Labels: it.Labels(), TimestampSec: it.At().Timestamp / 1e9, Value: it.At().Value, StreamHash: it.StreamHash()})
						}
						require.NoError(t, it.Err())
						require.NoError(t, it.Close())
						require.ElementsMatch(t, want, got, "%s, batch size %d", layout.name, batchSize)
					}
				}
			})
		}
	})

	t.Run("a real read that the consumer stops part-way closes without an error", func(t *testing.T) {
		const query = `count_over_time({app=~".+"}[1m])`

		var streams []logproto.Stream
		for s := range 3 {
			stream := logproto.Stream{Labels: fmt.Sprintf(`{app="app-%d"}`, s)}
			for i := range 40 {
				stream.Entries = append(stream.Entries, entry(t, i+1, "line"))
			}
			streams = append(streams, stream)
		}
		fixture := planReaderFixture(t, query, newObjectsFixtureWithOptions(t, []objtest.Option{objtest.WithTargetSectionSize(1 << 10)}, streams...))
		require.Greater(t, len(fixture.tasks), 1)

		for _, batchSize := range []int{1, 3, DefaultReadBatchSize} {
			for _, samplesRead := range []int{0, 1, 7, 100} {
				reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, batchSize, NewMetrics(nil))
				it := NewSampleIterator(reader, sampleExtractor(t, query))

				for range samplesRead {
					require.True(t, it.Next(), "batch size %d, %d samples read", batchSize, samplesRead)
				}
				require.NoError(t, it.Close(), "batch size %d, %d samples read", batchSize, samplesRead)
				require.NoError(t, it.Err(), "batch size %d, %d samples read", batchSize, samplesRead)
				require.False(t, it.Next(), "batch size %d, %d samples read", batchSize, samplesRead)
			}
		}
	})

	t.Run("a scan that fails to open its section ends the read with its error", func(t *testing.T) {
		const query = `count_over_time({app=~".+"}[1m])`

		fixture := newReaderFixture(t, query,
			logproto.Stream{Labels: `{app="a"}`, Entries: []logproto.Entry{entry(t, 1, "one"), entry(t, 2, "two")}},
			logproto.Stream{Labels: `{app="b"}`, Entries: []logproto.Entry{entry(t, 1, "three")}},
		)
		broken := fixture.tasks[0]
		broken.sectionIdx = 999
		tasks := append(slices.Clone(fixture.tasks), broken)

		reader := NewLogReader(t.Context(), fixture.objects, queuedTasks(tasks...), DefaultMaxConcurrency, 1, NewMetrics(nil))
		it := NewSampleIterator(reader, sampleExtractor(t, query))

		for it.Next() {
		}
		require.ErrorContains(t, it.Err(), "holds no logs section 999")
		require.ErrorContains(t, it.Close(), "holds no logs section 999")
		require.False(t, it.Next())
	})

	t.Run("a sample reads the same after its batch is released", func(t *testing.T) {
		skipWithoutPoisoning(t)
		batch := newTestBatch(t, pool, testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one", metadata: `{level="error"}`})
		it := newIterator(t, `count_over_time({app="a"}[1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		require.True(t, it.Next())
		sample, sampleLabels, streamHash := it.At(), it.Labels(), it.StreamHash()
		require.False(t, it.Next())
		require.True(t, batch.pooled)

		require.Equal(t, logproto.Sample{Timestamp: 1e9, Value: 1}, sample)
		require.Equal(t, `{app="a", level="error"}`, sampleLabels)
		require.Equal(t, streamHashOf(`{app="a"}`), streamHash)
	})

	t.Run("the rows after a line that the pipeline drops still yield samples, across streams", func(t *testing.T) {
		batch := newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "keep one"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "drop two"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 3, line: "keep three"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 4, line: "drop four"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 5, line: "keep five"},
		)
		it := newIterator(t, `count_over_time({app=~".+"} |= "keep" [1m])`, &fakeRowsBatchReader{batches: []*rowsBatch{batch}})

		var got []string
		for it.Next() {
			got = append(got, fmt.Sprintf("%s@%d", it.Labels(), it.At().Timestamp/1e9))
		}
		require.NoError(t, it.Err())
		require.Equal(t, []string{`{app="a"}@1`, `{app="a"}@3`, `{app="b"}@5`}, got)
		require.True(t, batch.pooled)
	})

	t.Run("rows from interleaved streams each report their own stream hash", func(t *testing.T) {
		reader := &fakeRowsBatchReader{batches: []*rowsBatch{newTestBatch(t, pool,
			testRow{streamLabels: `{app="a"}`, timestampSec: 1, line: "one"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 1, line: "two"},
			testRow{streamLabels: `{app="a"}`, timestampSec: 2, line: "three"},
			testRow{streamLabels: `{app="b"}`, timestampSec: 2, line: "four"},
		)}}
		it := newIterator(t, `count_over_time({app=~".+"}[1m])`, reader)

		var got []sampleRow
		for it.Next() {
			got = append(got, sampleRow{Labels: it.Labels(), TimestampSec: it.At().Timestamp / 1e9, Value: it.At().Value, StreamHash: it.StreamHash()})
		}
		require.NoError(t, it.Err())

		a, b := streamHashOf(`{app="a"}`), streamHashOf(`{app="b"}`)
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: a},
			{Labels: `{app="b"}`, TimestampSec: 1, Value: 1, StreamHash: b},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: a},
			{Labels: `{app="b"}`, TimestampSec: 2, Value: 1, StreamHash: b},
		}, got)
	})
}

// BenchmarkSampleIterator measures the read path from the sections to the samples, without the
// store and the range-vector layer above it.
func BenchmarkSampleIterator(b *testing.B) {
	queries := []struct{ name, expr string }{
		{"count", `sum(count_over_time({app=~".+"}[1m]))`},
		{"bytes", `sum(bytes_over_time({app=~".+"}[1m]))`},
		{"line-filter-keeps-every-line", `sum(count_over_time({app=~".+"} |= "level=" [1m]))`},
		{"logfmt-filter", `sum(count_over_time({app=~".+"} | logfmt | status="500" [1m]))`},
		{"metadata-filter", `sum(count_over_time({app=~".+"} | detected_level="error" [1m]))`},
	}
	shapes := []struct {
		name                    string
		streamCount, entryCount int
		sectionSize             flagext.Bytes
	}{
		{"streams=20,entries=10000,sections=one", 20, 10_000, 0},
		{"streams=2000,entries=100,sections=many", 2_000, 100, 1 << 20},
	}

	metrics := NewMetrics(nil)
	for _, shape := range shapes {
		var opts []objtest.Option
		if shape.sectionSize > 0 {
			opts = append(opts, objtest.WithTargetSectionSize(shape.sectionSize))
		}
		streams, _ := benchmarkStreams(shape.streamCount, shape.entryCount)
		objects := newObjectsFixtureWithOptions(b, opts, streams...)

		for _, query := range queries {
			fixture := planReaderFixture(b, query.expr, objects)

			b.Run(fmt.Sprintf("%s/query=%s", shape.name, query.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					reader := NewLogReader(b.Context(), fixture.objects, queuedTasks(fixture.tasks...), DefaultMaxConcurrency, DefaultReadBatchSize, metrics)
					it := NewSampleIterator(reader, sampleExtractor(b, query.expr))

					var samples int
					for it.Next() {
						_ = it.At()
						_ = it.Labels()
						samples++
					}
					require.NoError(b, it.Err())
					require.NoError(b, it.Close())
					require.Positive(b, samples)
					b.ReportMetric(float64(samples), "samples/op")
				}
			})
		}
	}
}

// sampleExtractor returns the extractor that the store builds for query: the extractor of the
// unpushed part, or of the whole query when the plan removes no line filter stage.
func sampleExtractor(t testing.TB, query string) syntax.SampleExtractor {
	t.Helper()

	expr, err := syntax.ParseSampleExpr(query)
	require.NoError(t, err)
	extractor, err := expr.Extractor()
	require.NoError(t, err)

	projection, err := NewProjectionPlan(expr, nil)
	require.NoError(t, err)
	if unpushed := projection.UnpushedExpr(); unpushed != nil {
		extractor, err = unpushed.Extractor()
		require.NoError(t, err)
	}
	return extractor
}

// fakeRowsBatchReader replays a fixed set of batches, so a test can drive the sample iterator with
// batches the read path would not produce.
type fakeRowsBatchReader struct {
	batches []*rowsBatch
	err     error

	// served holds the batches that the reader gave out. previousReleasedOnAsk has one element for
	// each call of nextRowsBatch: whether the iterator had released the previous batch by then.
	served                []*rowsBatch
	previousReleasedOnAsk []bool

	closed   bool
	closes   int
	closeErr error
}

func (r *fakeRowsBatchReader) nextRowsBatch() *rowsBatch {
	r.previousReleasedOnAsk = append(r.previousReleasedOnAsk, len(r.served) == 0 || r.served[len(r.served)-1].pooled)
	if len(r.batches) == 0 {
		return nil
	}
	batch := r.batches[0]
	r.batches = r.batches[1:]
	r.served = append(r.served, batch)
	return batch
}

func (r *fakeRowsBatchReader) Err() error { return r.err }

func (r *fakeRowsBatchReader) Close() error {
	r.closed = true
	r.closes++
	return r.closeErr
}

// testRow is one row of a batch that [newTestBatch] builds. A zero streamHash means the stable hash
// of the stream labels. streamLabels and metadata are LogQL label sets.
type testRow struct {
	streamLabels string
	streamHash   uint64
	timestampSec int64
	line         string
	metadata     string
}

// newTestBatch returns a batch from pool that holds the rows, with arrays in the memory of the
// batch. Consecutive rows of one stream form one run. The batch needs the lines.
func newTestBatch(t *testing.T, pool *rowsBatchPool, rows ...testRow) *rowsBatch {
	t.Helper()
	require.NotEmpty(t, rows)

	batch := pool.get()
	var (
		timestamps = columnar.NewNumberBuilder[int64](&batch.alloc)
		messages   = columnar.NewUTF8Builder(&batch.alloc)
		metadata   = make([]labels.Labels, len(rows))
		names      []string
		runs       []streamRun
	)
	for i, row := range rows {
		streamLabels, err := syntax.ParseLabels(row.streamLabels)
		require.NoError(t, err)
		streamHash := row.streamHash
		if streamHash == 0 {
			streamHash = labels.StableHash(streamLabels)
		}

		timestamps.AppendValue(row.timestampSec * 1e9)
		messages.AppendValue([]byte(row.line))

		metadata[i] = labels.EmptyLabels()
		if row.metadata != "" {
			metadata[i], err = syntax.ParseLabels(row.metadata)
			require.NoError(t, err)
		}
		metadata[i].Range(func(l labels.Label) {
			if !slices.Contains(names, l.Name) {
				names = append(names, l.Name)
			}
		})

		if n := len(runs); n > 0 && runs[n-1].stream.streamHash == streamHash && labels.Equal(runs[n-1].stream.streamLabels, streamLabels) {
			runs[n-1].end = i + 1
			continue
		}
		runs = append(runs, streamRun{stream: objectStream{streamLabels: streamLabels, streamHash: streamHash}, start: i, end: i + 1})
	}

	slices.Sort(names)
	columns := make([]logs.MetadataColumn, 0, len(names))
	for _, name := range names {
		values := columnar.NewUTF8Builder(&batch.alloc)
		for _, rowMetadata := range metadata {
			if !rowMetadata.Has(name) {
				values.AppendNull()
				continue
			}
			values.AppendValue([]byte(rowMetadata.Get(name)))
		}
		columns = append(columns, logs.MetadataColumn{Name: name, Values: values.Build()})
	}

	batch.records = &logs.RecordBatch{
		Rows:      len(rows),
		Timestamp: timestamps.BuildArray().(*columnar.Number[int64]),
		Message:   messages.Build(),
		Metadata:  columns,
	}
	batch.streamRuns = runs
	batch.hasLine = true
	batch.hasMetadata = len(columns) > 0
	return batch
}
