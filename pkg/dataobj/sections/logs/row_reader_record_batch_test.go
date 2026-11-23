package logs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// spanningPagesOptions are builder options that make pages of about 1 KiB. A batch of 100 of the
// records of spanningPagesRecords spans pages.
var spanningPagesOptions = BuilderOptions{StripeMergeLimit: 2, SortOrder: SortStreamASC, PageSizeHint: 1024}

func TestRowReader_ReadRecordBatch(t *testing.T) {
	t.Run("returns the records of the section as one array for each column", func(t *testing.T) {
		section := buildTestProjectionSection(t)

		reader := NewRowReader(section)
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 100)
		require.NoError(t, err)

		require.Equal(t, 3, batch.Rows)
		require.Equal(t, []int64{1, 2, 2}, batch.StreamID.Values())

		// The section sorts the rows of a stream by descending timestamp.
		require.Equal(t, []int64{10e9, 30e9, 20e9}, batch.Timestamp.Values())
		require.Equal(t, "line-1", string(batch.Message.Get(0)))
		require.Equal(t, "line-3", string(batch.Message.Get(1)))
		require.Equal(t, "line-2", string(batch.Message.Get(2)))
		require.Len(t, batch.Metadata, 2)
		require.Equal(t, "env", batch.Metadata[0].Name)
		require.Equal(t, "trace_id", batch.Metadata[1].Name)

		scratch := labels.NewScratchBuilder(0)
		require.Equal(t, `{env="prod", trace_id="t1"}`, batch.MetadataLabels(0, &scratch).String())
		require.Equal(t, `{env="dev", trace_id="t3"}`, batch.MetadataLabels(1, &scratch).String())
		require.Equal(t, `{env="prod", trace_id="t2"}`, batch.MetadataLabels(2, &scratch).String())

		_, err = reader.ReadRecordBatch(t.Context(), &alloc, 100)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("returns the same records as Read", func(t *testing.T) {
		records := randomRecords(rand.New(rand.NewSource(1)), 3000)
		sections := map[string]*Section{
			"sorted by stream":    buildSectionFromRecords(t, SortStreamASC, records),
			"sorted by timestamp": buildSectionFromRecords(t, SortTimestampDESC, records),
		}

		projections := []struct {
			name          string
			types         []ColumnType
			metadataNames []string
		}{
			{name: "every column"},
			{name: "stream ID and timestamp", types: []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp}},
			{name: "with the message", types: []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMessage}},
			{name: "with one metadata key", types: []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp}, metadataNames: []string{"env"}},
			{name: "with every metadata key", types: []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMetadata}},
			{name: "with a metadata key that the section lacks", types: []ColumnType{ColumnTypeTimestamp}, metadataNames: []string{"missing"}},
		}

		lineHasError := func(line []byte) bool { return bytes.Contains(line, []byte("error")) }
		predicates := []struct {
			name       string
			streams    []int64
			predicates []RowPredicate
		}{
			{name: "no predicate"},
			{name: "a matched stream", streams: []int64{2, 4}},
			{name: "a time range that includes the start", predicates: []RowPredicate{TimeRangeRowPredicate{StartTime: records[100].Timestamp, EndTime: records[2000].Timestamp, IncludeStart: true}}},
			{name: "a time range that includes the end", predicates: []RowPredicate{TimeRangeRowPredicate{StartTime: records[100].Timestamp, EndTime: records[2000].Timestamp, IncludeEnd: true}}},
			{name: "a time range that holds no record", predicates: []RowPredicate{TimeRangeRowPredicate{StartTime: unixSecond(5), EndTime: unixSecond(4), IncludeStart: true}}},
			{name: "a metadata equality", predicates: []RowPredicate{MetadataMatcherRowPredicate{Key: "env", Value: "prod"}}},
			{name: "a metadata equality on an empty value", predicates: []RowPredicate{MetadataMatcherRowPredicate{Key: "env", Value: ""}}},
			{name: "a metadata equality on a missing key", predicates: []RowPredicate{MetadataMatcherRowPredicate{Key: "missing", Value: "x"}}},
			{name: "a metadata filter", predicates: []RowPredicate{MetadataFilterRowPredicate{Key: "region", Keep: func(_, value string) bool { return strings.HasPrefix(value, "eu") }}}},
			{name: "a negated metadata filter", predicates: []RowPredicate{MetadataFilterRowPredicate{Key: "region", Keep: func(_, value string) bool { return value != "us-east" }}}},
			{name: "a line filter", predicates: []RowPredicate{LogMessageFilterRowPredicate{Keep: lineHasError}}},
			{name: "a line filter with a batch function", predicates: []RowPredicate{LogMessageFilterRowPredicate{Keep: lineHasError, KeepBatch: keepBatchOf(lineHasError)}}},
			{name: "a line filter that keeps no line", predicates: []RowPredicate{LogMessageFilterRowPredicate{Keep: func([]byte) bool { return false }, KeepBatch: keepBatchOf(func([]byte) bool { return false })}}},
			{name: "a line filter after a metadata equality", predicates: []RowPredicate{
				MetadataMatcherRowPredicate{Key: "env", Value: "prod"},
				LogMessageFilterRowPredicate{Keep: lineHasError, KeepBatch: keepBatchOf(lineHasError)},
			}},
			{name: "an or of a metadata equality and a line filter", predicates: []RowPredicate{OrRowPredicate{
				Left:  MetadataMatcherRowPredicate{Key: "env", Value: "dev"},
				Right: LogMessageFilterRowPredicate{Keep: lineHasError, KeepBatch: keepBatchOf(lineHasError)},
			}}},
			{name: "a negated line filter", predicates: []RowPredicate{NotRowPredicate{Inner: LogMessageFilterRowPredicate{Keep: lineHasError}}}},
			{name: "streams, a time range, a metadata equality and a line filter", streams: []int64{1, 2, 3}, predicates: []RowPredicate{
				TimeRangeRowPredicate{StartTime: unixSecond(500), EndTime: unixSecond(2500), IncludeStart: true},
				MetadataMatcherRowPredicate{Key: "env", Value: "prod"},
				LogMessageFilterRowPredicate{Keep: lineHasError, KeepBatch: keepBatchOf(lineHasError)},
			}},
		}

		const batchSize = 7
		for sectionName, section := range sections {
			for _, projection := range projections {
				for _, predicate := range predicates {
					t.Run(fmt.Sprintf("%s/%s/%s", sectionName, projection.name, predicate.name), func(t *testing.T) {
						open := func() *RowReader {
							r := NewRowReader(section)
							if projection.types != nil || projection.metadataNames != nil {
								require.NoError(t, r.SetProjectedColumns(projection.types, projection.metadataNames))
							}
							if predicate.streams != nil {
								require.NoError(t, r.MatchStreams(slices.Values(predicate.streams)))
							}
							require.NoError(t, r.SetPredicates(predicate.predicates))
							require.NoError(t, r.Open(t.Context()))
							return r
						}

						reader := open()
						want := readAllRecords(t, reader)
						require.NoError(t, reader.Close())

						reader = open()
						got := readAllBatchRecords(t, reader, batchSize)
						require.NoError(t, reader.Close())

						requireSameRecords(t, want, got)
					})
				}
			}
		}
	})

	t.Run("returns the message as a view over the pages when the batch spans pages and no message is null", func(t *testing.T) {
		records := randomRecords(rand.New(rand.NewSource(2)), 400)
		for i := range records {
			records[i].Line = fmt.Appendf(nil, "line %d %s", i, strings.Repeat("x", 100))
		}
		section := buildSectionWithOptions(t, spanningPagesOptions, records)

		reader := NewRowReader(section)
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 100)
		require.NoError(t, err)

		view, ok := batch.Message.(*columnar.ChunkedUTF8)
		require.True(t, ok, "got %T, want a view", batch.Message)
		require.Greater(t, view.NumChunks(), 1)

		wantReader := NewRowReader(section)
		require.NoError(t, wantReader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, wantReader.Close()) })
		requireSameRecords(t, readAllRecords(t, wantReader)[:100], batchRecords(batch))
	})

	t.Run("returns the same records as Read for a line filter with a batch function when the batches span pages", func(t *testing.T) {
		section := buildSectionWithOptions(t, spanningPagesOptions, spanningPagesRecords(400))
		lineHasError := func(line []byte) bool { return bytes.Contains(line, []byte("level=error")) }
		lineFilter := LogMessageFilterRowPredicate{Keep: lineHasError, KeepBatch: keepBatchOf(lineHasError)}

		for name, predicates := range map[string][]RowPredicate{
			"alone": {lineFilter},
			"after a selective metadata equality": {
				MetadataMatcherRowPredicate{Key: "env", Value: "env-1-" + strings.Repeat("m", 60)},
				lineFilter,
			},
		} {
			for _, batchSize := range []int{7, 100, 1000} {
				t.Run(fmt.Sprintf("%s with a batch size of %d", name, batchSize), func(t *testing.T) {
					open := func() *RowReader {
						r := NewRowReader(section)
						require.NoError(t, r.SetPredicates(predicates))
						require.NoError(t, r.Open(t.Context()))
						return r
					}

					reader := open()
					want := readAllRecords(t, reader)
					require.NoError(t, reader.Close())
					require.NotEmpty(t, want)

					reader = open()
					got := readAllBatchRecords(t, reader, batchSize)
					require.NoError(t, reader.Close())

					requireSameRecords(t, want, got)
				})
			}
		}
	})

	t.Run("returns a metadata column as a view when its key is set on every record and the batch spans pages", func(t *testing.T) {
		section := buildSectionWithOptions(t, spanningPagesOptions, spanningPagesRecords(400))

		reader := NewRowReader(section)
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 100)
		require.NoError(t, err)

		require.Len(t, batch.Metadata, 1)
		require.Equal(t, "env", batch.Metadata[0].Name)
		require.IsType(t, &columnar.ChunkedUTF8{}, batch.Metadata[0].Values)

		wantReader := NewRowReader(section)
		require.NoError(t, wantReader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, wantReader.Close()) })
		requireSameRecords(t, readAllRecords(t, wantReader)[:100], batchRecords(batch))
	})

	t.Run("returns every record once and in order when Read and ReadRecordBatch alternate", func(t *testing.T) {
		section := buildSectionFromRecords(t, SortStreamASC, randomRecords(rand.New(rand.NewSource(2)), 500))

		reference := NewRowReader(section)
		require.NoError(t, reference.Open(t.Context()))
		want := readAllRecords(t, reference)
		require.NoError(t, reference.Close())

		reader := NewRowReader(section)
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var (
			got   []Record
			alloc memory.Allocator
			rows  = make([]Record, 5)
		)
		for i := 0; ; i++ {
			if i%2 == 0 {
				n, err := reader.Read(t.Context(), rows)
				for j := range rows[:n] {
					got = append(got, rows[j].Copy())
				}
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				continue
			}

			alloc.Reclaim()
			batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 7)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			got = append(got, batchRecords(batch)...)
		}

		requireSameRecords(t, want, got)
	})

	t.Run("returns every record in one batch when the batch size exceeds the section", func(t *testing.T) {
		records := randomRecords(rand.New(rand.NewSource(3)), 1000)
		section := buildSectionFromRecords(t, SortStreamASC, records)

		reference := NewRowReader(section)
		require.NoError(t, reference.Open(t.Context()))
		want := readAllRecords(t, reference)
		require.NoError(t, reference.Close())

		reader := NewRowReader(section)
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 100_000)
		require.NoError(t, err)
		require.Equal(t, len(want), batch.Rows)
	})

	t.Run("returns io.EOF at the end of the section and on every later call", func(t *testing.T) {
		section := buildTestSection(t)

		reader := NewRowReader(section)
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.NoError(t, err)
		require.Equal(t, 2, batch.Rows)

		for range 3 {
			batch, err = reader.ReadRecordBatch(t.Context(), &alloc, 10)
			require.Nil(t, batch)
			require.ErrorIs(t, err, io.EOF)
		}
	})

	t.Run("returns an error before the reader is opened", func(t *testing.T) {
		reader := NewRowReader(buildTestSection(t))

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.Nil(t, batch)
		require.ErrorIs(t, err, errRowReaderNotOpen)
	})

	t.Run("returns io.EOF for a reader without a section", func(t *testing.T) {
		reader := NewRowReader(nil)
		require.NoError(t, reader.Open(t.Context()))

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.Nil(t, batch)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("returns an error when the batch size is not positive", func(t *testing.T) {
		reader := NewRowReader(buildTestSection(t))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		_, err := reader.ReadRecordBatch(t.Context(), &alloc, 0)
		require.EqualError(t, err, "reading record batch: batch size must be positive, got 0")
	})

	t.Run("returns the error of the context when it is done", func(t *testing.T) {
		reader := NewRowReader(buildTestSection(t))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var alloc memory.Allocator
		_, err := reader.ReadRecordBatch(ctx, &alloc, 10)
		require.ErrorIs(t, err, ctx.Err())
	})

	t.Run("leaves the arrays of the columns that it does not project nil", func(t *testing.T) {
		reader := NewRowReader(buildTestProjectionSection(t))
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeTimestamp}, []string{"env"}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.NoError(t, err)

		require.Nil(t, batch.StreamID)
		require.Nil(t, batch.Message)
		require.NotNil(t, batch.Timestamp)
		require.Len(t, batch.Metadata, 1)
		require.Equal(t, "env", batch.Metadata[0].Name)
	})

	t.Run("reads the columns that a predicate needs without returning them", func(t *testing.T) {
		reader := NewRowReader(buildTestProjectionSection(t))
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeTimestamp}, nil))
		require.NoError(t, reader.SetPredicates([]RowPredicate{MetadataMatcherRowPredicate{Key: "env", Value: "dev"}}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.NoError(t, err)

		require.Equal(t, 1, batch.Rows)
		require.Equal(t, []int64{30e9}, batch.Timestamp.Values())
		require.Empty(t, batch.Metadata)
	})

	t.Run("returns the columns of the new section after a reset with another projection", func(t *testing.T) {
		reader := NewRowReader(buildTestProjectionSection(t))
		require.NoError(t, reader.Open(t.Context()))

		var alloc memory.Allocator
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.NoError(t, err)
		require.Equal(t, 3, batch.Rows)
		require.Len(t, batch.Metadata, 2)
		require.NoError(t, reader.Close())

		alloc.Reclaim()
		reader.Reset(buildTestSection(t))
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeTimestamp}, nil))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		batch, err = reader.ReadRecordBatch(t.Context(), &alloc, 10)
		require.NoError(t, err)
		require.Equal(t, 2, batch.Rows)
		require.NotNil(t, batch.Timestamp)
		require.Nil(t, batch.StreamID)
		require.Nil(t, batch.Message)
		require.Empty(t, batch.Metadata)
	})
}

func TestRecordBatch_MetadataLabels(t *testing.T) {
	scratch := labels.NewScratchBuilder(0)

	t.Run("returns no labels when the batch holds no metadata column", func(t *testing.T) {
		batch := &RecordBatch{Rows: 1}
		require.True(t, batch.MetadataLabels(0, &scratch).IsEmpty())
	})

	t.Run("skips a null element and keeps an empty one", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		builder := columnar.NewUTF8Builder(alloc)
		builder.AppendNull()
		builder.AppendValue([]byte(""))
		builder.AppendValue([]byte("v"))
		values := builder.BuildArray().(*columnar.UTF8)

		batch := &RecordBatch{Rows: 3, Metadata: []MetadataColumn{{Name: "key", Values: values}}}
		require.True(t, batch.MetadataLabels(0, &scratch).IsEmpty())
		require.Equal(t, labels.FromStrings("key", ""), batch.MetadataLabels(1, &scratch))
		require.Equal(t, labels.FromStrings("key", "v"), batch.MetadataLabels(2, &scratch))
	})

	t.Run("keeps the labels of earlier calls valid when it reuses the scratch builder", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		builder := columnar.NewUTF8Builder(alloc)
		builder.AppendValue([]byte("first"))
		builder.AppendValue([]byte("second"))
		values := builder.BuildArray().(*columnar.UTF8)

		batch := &RecordBatch{Rows: 2, Metadata: []MetadataColumn{{Name: "key", Values: values}}}
		first := batch.MetadataLabels(0, &scratch)
		second := batch.MetadataLabels(1, &scratch)

		require.Equal(t, labels.FromStrings("key", "first"), first)
		require.Equal(t, labels.FromStrings("key", "second"), second)
	})

	t.Run("returns the labels in the order of the columns", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		build := func(value string) *columnar.UTF8 {
			builder := columnar.NewUTF8Builder(alloc)
			builder.AppendValue([]byte(value))
			return builder.BuildArray().(*columnar.UTF8)
		}

		batch := &RecordBatch{Rows: 1, Metadata: []MetadataColumn{{Name: "a", Values: build("1")}, {Name: "b", Values: build("2")}}}
		require.Equal(t, labels.FromStrings("a", "1", "b", "2"), batch.MetadataLabels(0, &scratch))
	})
}

func TestRowReader_newRecordBatch(t *testing.T) {
	newReader := func() *RowReader {
		return &RowReader{
			projSectionColumns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeTimestamp},
				{Type: ColumnTypeMessage},
				{Type: ColumnTypeMetadata, Name: "env"},
			},
			projMetadataColumns: 1,
		}
	}

	alloc := memory.NewAllocator(nil)
	numbers := func(values ...int64) columnar.Array {
		builder := columnar.NewNumberBuilder[int64](alloc)
		for _, value := range values {
			builder.AppendValue(value)
		}
		return builder.BuildArray()
	}
	strs := func(values ...string) columnar.Array {
		builder := columnar.NewUTF8Builder(alloc)
		for _, value := range values {
			builder.AppendValue([]byte(value))
		}
		return builder.BuildArray()
	}

	t.Run("sets the field of each column from the array at its position", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{numbers(1, 2), numbers(10, 20), strs("a", "b"), strs("x", "y")})

		batch, err := newReader().newRecordBatch(rb)
		require.NoError(t, err)

		require.Equal(t, 2, batch.Rows)
		require.Equal(t, []int64{1, 2}, batch.StreamID.Values())
		require.Equal(t, []int64{10, 20}, batch.Timestamp.Values())
		require.Equal(t, "b", string(batch.Message.Get(1)))
		require.Len(t, batch.Metadata, 1)
		require.Equal(t, "env", batch.Metadata[0].Name)
		require.Equal(t, "y", string(batch.Metadata[0].Values.Get(1)))
	})

	t.Run("sorts the metadata columns by name when the section orders them otherwise", func(t *testing.T) {
		reader := &RowReader{
			projSectionColumns: []*Column{
				{Type: ColumnTypeMetadata, Name: "b"},
				{Type: ColumnTypeMetadata, Name: "a"},
			},
			projMetadataColumns: 2,
		}
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{strs("2"), strs("1")})

		batch, err := reader.newRecordBatch(rb)
		require.NoError(t, err)

		require.Equal(t, "a", batch.Metadata[0].Name)
		require.Equal(t, "b", batch.Metadata[1].Name)

		scratch := labels.NewScratchBuilder(0)
		require.Equal(t, labels.FromStrings("a", "1", "b", "2"), batch.MetadataLabels(0, &scratch))
	})

	t.Run("keeps a null message and a null metadata element", func(t *testing.T) {
		messages := columnar.NewUTF8Builder(alloc)
		messages.AppendNull()
		env := columnar.NewUTF8Builder(alloc)
		env.AppendNull()
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{numbers(1), numbers(10), messages.BuildArray(), env.BuildArray()})

		batch, err := newReader().newRecordBatch(rb)
		require.NoError(t, err)
		require.True(t, batch.Message.IsNull(0))
		require.True(t, batch.Metadata[0].Values.IsNull(0))
	})

	t.Run("keeps a view for the message and for a metadata column", func(t *testing.T) {
		view := func(chunks ...string) columnar.Array {
			arrays := make([]*columnar.UTF8, len(chunks))
			for i, chunk := range chunks {
				arrays[i] = strs(chunk).(*columnar.UTF8)
			}
			return columnar.NewChunkedUTF8(arrays)
		}
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{numbers(1, 2), numbers(10, 20), view("a", "b"), view("x", "y")})

		batch, err := newReader().newRecordBatch(rb)
		require.NoError(t, err)

		require.Equal(t, "b", string(batch.Message.Get(1)))
		scratch := labels.NewScratchBuilder(0)
		require.Equal(t, labels.FromStrings("env", "y"), batch.MetadataLabels(1, &scratch))
	})

	t.Run("returns an error when the record batch has no row", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 0, []columnar.Array{numbers(), numbers(), strs(), strs()})

		_, err := newReader().newRecordBatch(rb)
		require.EqualError(t, err, "record batch has no row")
	})

	t.Run("returns an error when the record batch has another number of columns", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{numbers(1), numbers(10)})

		_, err := newReader().newRecordBatch(rb)
		require.EqualError(t, err, "record batch has 2 columns, want 4")
	})

	t.Run("returns an error when an array has another number of elements than the record batch", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{numbers(1, 2), numbers(10), strs("a", "b"), strs("x", "y")})

		_, err := newReader().newRecordBatch(rb)
		require.EqualError(t, err, "array of timestamp has 1 elements, want 2")
	})

	t.Run("returns an error when an array has another type than its column", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{strs("1"), numbers(10), strs("a"), strs("x")})

		_, err := newReader().newRecordBatch(rb)
		require.EqualError(t, err, "invalid array type *columnar.UTF8 for stream_id")
	})

	t.Run("returns an error when the message array is not a UTF8 array", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{numbers(1), numbers(10), numbers(5), strs("x")})

		_, err := newReader().newRecordBatch(rb)
		require.EqualError(t, err, "invalid array type *columnar.Number[int64] for message")
	})

	t.Run("returns an error when the stream ID array or the timestamp array holds a null", func(t *testing.T) {
		withNull := func() columnar.Array {
			builder := columnar.NewNumberBuilder[int64](alloc)
			builder.AppendNull()
			return builder.BuildArray()
		}

		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{withNull(), numbers(10), strs("a"), strs("x")})
		_, err := newReader().newRecordBatch(rb)
		require.EqualError(t, err, "array of stream_id has 1 null elements")

		rb = columnar.NewRecordBatch(nil, 1, []columnar.Array{numbers(1), withNull(), strs("a"), strs("x")})
		_, err = newReader().newRecordBatch(rb)
		require.EqualError(t, err, "array of timestamp has 1 null elements")
	})
}

// randomRecords returns n records across six streams, with a time range of about an hour, lines
// that mention a level, empty lines, and metadata that some records lack.
func randomRecords(rng *rand.Rand, n int) []Record {
	var (
		levels  = []string{"info", "warn", "error", "debug"}
		envs    = []string{"prod", "dev", ""}
		regions = []string{"us-east", "eu-west", "eu-north", "ap-south"}
	)

	records := make([]Record, 0, n)
	for i := range n {
		record := Record{
			StreamID:  int64(1 + rng.Intn(6)),
			Timestamp: time.Unix(int64(i)*3600/int64(n), int64(rng.Intn(1_000_000))).UTC(),
			Line:      fmt.Appendf(nil, "level=%s msg=request id=%d", levels[rng.Intn(len(levels))], rng.Intn(1000)),
		}
		if rng.Intn(20) == 0 {
			record.Line = nil
		}

		var pairs []string
		if rng.Intn(4) != 0 {
			pairs = append(pairs, "env", envs[rng.Intn(len(envs))])
		}
		if rng.Intn(3) != 0 {
			pairs = append(pairs, "region", regions[rng.Intn(len(regions))])
		}
		if rng.Intn(10) == 0 {
			pairs = append(pairs, "trace_id", fmt.Sprintf("%016x", rng.Uint64()))
		}
		record.Metadata = labels.FromStrings(pairs...)

		records = append(records, record)
	}
	return records
}

// readAllBatchRecords drains reader with ReadRecordBatch into records. It decodes each batch into
// records and then reclaims the memory of the batch, as a caller of the reader does.
func readAllBatchRecords(t *testing.T, reader *RowReader, batchSize int) []Record {
	t.Helper()

	var (
		out   []Record
		alloc memory.Allocator
	)
	for {
		alloc.Reclaim()
		batch, err := reader.ReadRecordBatch(t.Context(), &alloc, batchSize)
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		require.NotZero(t, batch.Rows)
		require.LessOrEqual(t, batch.Rows, batchSize)

		out = append(out, batchRecords(batch)...)
	}
}

// batchRecords returns the records of batch. The records own their lines and labels, so they stay
// valid after the caller reclaims the memory of the batch.
func batchRecords(batch *RecordBatch) []Record {
	scratch := labels.NewScratchBuilder(len(batch.Metadata))
	records := make([]Record, 0, batch.Rows)
	for i := range batch.Rows {
		var record Record
		if batch.StreamID != nil {
			record.StreamID = batch.StreamID.Get(i)
		}
		if batch.Timestamp != nil {
			record.Timestamp = time.Unix(0, batch.Timestamp.Get(i)).UTC()
		}
		if batch.Message != nil {
			record.Line = bytes.Clone(batch.Message.Get(i))
		}
		record.Metadata = batch.MetadataLabels(i, &scratch)
		records = append(records, record)
	}
	return records
}

// requireSameRecords fails the test unless got holds the records of want, in the same order. A
// nil line and an empty line are the same.
func requireSameRecords(t *testing.T, want, got []Record) {
	t.Helper()

	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].StreamID, got[i].StreamID, "record %d: stream ID", i)
		require.Equal(t, want[i].Timestamp.UnixNano(), got[i].Timestamp.UnixNano(), "record %d: timestamp", i)
		require.Equal(t, string(want[i].Line), string(got[i].Line), "record %d: line", i)
		require.True(t, labels.Equal(want[i].Metadata, got[i].Metadata), "record %d: metadata %s and %s", i, want[i].Metadata, got[i].Metadata)
	}
}

// spanningPagesRecords returns n records in six streams. Every record has a line and an env
// metadata value, so no array of the section holds a null.
func spanningPagesRecords(n int) []Record {
	levels := []string{"info", "warn", "error", "debug"}

	records := make([]Record, 0, n)
	for i := range n {
		records = append(records, Record{
			StreamID:  int64(1 + i%6),
			Timestamp: time.Unix(int64(i), 0).UTC(),
			Line:      fmt.Appendf(nil, "line %d level=%s %s", i, levels[(i/4)%len(levels)], strings.Repeat("x", 100)),
			Metadata:  labels.FromStrings("env", fmt.Sprintf("env-%d-%s", i%4, strings.Repeat("m", 60))),
		})
	}
	return records
}
