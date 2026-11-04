package logs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestRowReader_NoPredicates(t *testing.T) {
	logsSection := buildTestSection(t)

	readBuf := make([]Record, 3)
	rowReader := NewRowReader(logsSection)
	require.NoError(t, rowReader.Open(context.Background()))
	n, err := rowReader.Read(context.Background(), readBuf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestRowReader_StreamIDPredicate(t *testing.T) {
	// recordsForStreams returns one record per stream ID, in the given order. Each record has a
	// distinct timestamp and a line that names its stream and position.
	recordsForStreams := func(streamIDs ...int64) []Record {
		start := time.Unix(1_700_000_000, 0)
		records := make([]Record, len(streamIDs))
		for i, id := range streamIDs {
			records[i] = Record{
				StreamID:  id,
				Timestamp: start.Add(time.Duration(i) * time.Second),
				Line:      fmt.Appendf(nil, "stream=%d row=%d", id, i),
			}
		}
		return records
	}

	// linesOfStreams returns the sorted lines of the records that belong to the given streams.
	linesOfStreams := func(records []Record, streamIDs ...int64) []string {
		var lines []string
		for _, record := range records {
			if slices.Contains(streamIDs, record.StreamID) {
				lines = append(lines, string(record.Line))
			}
		}
		slices.Sort(lines)
		return lines
	}

	// readLinesOf reads the rows of the given streams from a section and returns their sorted
	// lines. It takes no testing.T, so goroutines can call it.
	readLinesOf := func(ctx context.Context, section *Section, streamIDs ...int64) ([]string, error) {
		reader := NewRowReader(section)
		defer reader.Close()

		if err := reader.MatchStreams(slices.Values(streamIDs)); err != nil {
			return nil, err
		}
		if err := reader.Open(ctx); err != nil {
			return nil, err
		}

		var lines []string
		buf := make([]Record, 4)
		for {
			n, err := reader.Read(ctx, buf)
			for _, record := range buf[:n] {
				lines = append(lines, string(record.Line))
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
		}
		slices.Sort(lines)
		return lines, nil
	}

	// readLines is readLinesOf, but it fails the test on an error.
	readLines := func(t *testing.T, section *Section, streamIDs ...int64) []string {
		t.Helper()

		lines, err := readLinesOf(t.Context(), section, streamIDs...)
		require.NoError(t, err)
		return lines
	}

	t.Run("returns the row of the matched stream", func(t *testing.T) {
		logsSection := buildTestSection(t)

		readBuf := make([]Record, 3)
		rowReader := NewRowReader(logsSection)

		err := rowReader.MatchStreams(slices.Values([]int64{1}))
		require.NoError(t, err)
		require.NoError(t, rowReader.Open(t.Context()))
		n, err := rowReader.Read(t.Context(), readBuf)
		require.NoError(t, err)
		require.Equal(t, 1, n)
	})

	t.Run("returns every row of the matched streams and none of the streams between them when each stream's rows are contiguous", func(t *testing.T) {
		records := recordsForStreams(1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5, 5)
		section := buildSectionFromRecords(t, SortStreamASC, records)

		require.Equal(t, linesOfStreams(records, 2, 4), readLines(t, section, 2, 4))
	})

	t.Run("returns every row of the matched streams when stream IDs interleave", func(t *testing.T) {
		records := recordsForStreams(1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4)
		section := buildSectionFromRecords(t, SortTimestampDESC, records)

		require.Equal(t, linesOfStreams(records, 2, 3), readLines(t, section, 2, 3))
	})

	t.Run("returns no row when no stream of the section matches", func(t *testing.T) {
		records := recordsForStreams(1, 1, 1, 2, 2, 2)
		section := buildSectionFromRecords(t, SortStreamASC, records)

		require.Empty(t, readLines(t, section, 99))
	})

	t.Run("returns the same rows for sections sorted by stream and by timestamp", func(t *testing.T) {
		records := recordsForStreams(1, 2, 3, 1, 2, 3, 1, 1, 2, 3, 3, 3)
		byStream := buildSectionFromRecords(t, SortStreamASC, records)
		byTimestamp := buildSectionFromRecords(t, SortTimestampDESC, records)

		want := linesOfStreams(records, 1, 3)
		require.Equal(t, want, readLines(t, byStream, 1, 3))
		require.Equal(t, want, readLines(t, byTimestamp, 1, 3))
	})

	t.Run("gives every reader of one section the full result when many goroutines read it at once", func(t *testing.T) {
		records := recordsForStreams(1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4)
		section := buildSectionFromRecords(t, SortStreamASC, records)
		want := linesOfStreams(records, 2, 3)

		const readers = 8
		var (
			wg      sync.WaitGroup
			results = make([][]string, readers)
			errs    = make([]error, readers)
		)
		for i := range readers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = readLinesOf(t.Context(), section, 2, 3)
			}()
		}
		wg.Wait()

		for i := range readers {
			require.NoError(t, errs[i])
			require.Equal(t, want, results[i], "reader %d", i)
		}
	})
}

func TestRowReader_ReadBeforeOpen(t *testing.T) {
	logsSection := buildTestSection(t)
	rowReader := NewRowReader(logsSection)

	readBuf := make([]Record, 1)
	n, err := rowReader.Read(context.Background(), readBuf)
	require.Zero(t, n)
	require.ErrorContains(t, err, "row reader not opened")
}

func TestRowReader_SetProjectedColumns(t *testing.T) {
	section := buildTestProjectionSection(t)

	t.Run("the Record carries only the projected metadata", func(t *testing.T) {
		// Stream 1 carries both metadata keys, so the projection decides which of them, and
		// the line, come back in the Record.
		for _, tc := range []struct {
			name         string
			types        []ColumnType
			metadata     []string
			wantMetadata string
			wantLine     string
		}{
			{
				name:         "stream ID and timestamp only",
				types:        []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp},
				wantMetadata: "{}",
			},
			{
				name:         "all metadata without the message",
				types:        []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMetadata},
				wantMetadata: `{env="prod", trace_id="t1"}`,
			},
			{
				name:         "one metadata column by name",
				types:        []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp},
				metadata:     []string{"trace_id"},
				wantMetadata: `{trace_id="t1"}`,
			},
			{
				name:         "a named metadata column the section does not hold",
				types:        []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp},
				metadata:     []string{"missing"},
				wantMetadata: "{}",
			},
			{
				name:         "the message with no metadata",
				types:        []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMessage},
				wantMetadata: "{}",
				wantLine:     "line-1",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				reader := NewRowReader(section)
				require.NoError(t, reader.SetProjectedColumns(tc.types, tc.metadata))
				require.NoError(t, reader.MatchStreams(slices.Values([]int64{1})))
				require.NoError(t, reader.Open(context.Background()))
				t.Cleanup(func() { require.NoError(t, reader.Close()) })

				got := readAllRecords(t, reader)
				require.Len(t, got, 1)
				require.Equal(t, int64(1), got[0].StreamID)
				require.Equal(t, tc.wantMetadata, got[0].Metadata.String())
				require.Equal(t, tc.wantLine, string(got[0].Line))
			})
		}
	})

	t.Run("a field whose column is not read is zeroed, not left stale", func(t *testing.T) {
		// A caller reuses its Record buffer across reads, so a leftover value would pass for
		// a real one.
		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeMessage}, nil))
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		buf := make([]Record, 1)
		buf[0] = Record{
			StreamID:  42,
			Timestamp: unixSecond(999),
			Metadata:  labels.FromStrings("stale", "yes"),
			Line:      []byte("stale line"),
		}

		n, err := reader.Read(context.Background(), buf)
		require.NoError(t, err)
		require.Equal(t, 1, n)

		require.Equal(t, int64(1), buf[0].StreamID)
		require.Equal(t, "line-1", string(buf[0].Line))
		require.True(t, buf[0].Timestamp.IsZero(), "an unprojected timestamp must be zeroed, got %s", buf[0].Timestamp)
		require.Equal(t, labels.EmptyLabels(), buf[0].Metadata)
	})

	t.Run("a column a predicate needs is read and not decoded when it is not projected", func(t *testing.T) {
		// The projection lists neither the timestamp nor the metadata keys these predicates
		// filter on, so each must still be evaluated against its own column instead of
		// collapsing to a constant. The message is projected because the assertions read it.
		for _, tc := range []struct {
			name      string
			predicate RowPredicate
			wantLines []string
		}{
			{
				name: "time range",
				predicate: TimeRangeRowPredicate{
					StartTime:    unixSecond(15),
					EndTime:      unixSecond(25),
					IncludeStart: true,
				},
				wantLines: []string{"line-2"},
			},
			{
				name:      "metadata equality",
				predicate: MetadataMatcherRowPredicate{Key: "trace_id", Value: "t2"},
				wantLines: []string{"line-2"},
			},
			{
				name: "metadata filter",
				predicate: MetadataFilterRowPredicate{Key: "env", Keep: func(_, value string) bool {
					return value == "dev"
				}},
				wantLines: []string{"line-3"},
			},
			{
				name: "message filter",
				predicate: LogMessageFilterRowPredicate{Keep: func(line []byte) bool {
					return string(line) == "line-3"
				}},
				wantLines: []string{"line-3"},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				reader := NewRowReader(section)
				require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeMessage}, nil))
				require.NoError(t, reader.SetPredicates([]RowPredicate{tc.predicate}))
				require.NoError(t, reader.Open(context.Background()))
				t.Cleanup(func() { require.NoError(t, reader.Close()) })

				var lines []string
				for _, rec := range readAllRecords(t, reader) {
					lines = append(lines, string(rec.Line))
					require.True(t, rec.Timestamp.IsZero(), "an unprojected timestamp must not be decoded, got %s", rec.Timestamp)
					require.Equal(t, labels.EmptyLabels(), rec.Metadata)
				}
				require.Equal(t, tc.wantLines, lines)
			})
		}
	})

	t.Run("leaving the stream ID out of the projection does not drop rows", func(t *testing.T) {
		// A projection selects columns; it must never filter. With no stream match and no
		// predicates nothing narrows this read, so every record comes back.
		for _, tc := range []struct {
			name  string
			types []ColumnType
		}{
			{"message only", []ColumnType{ColumnTypeMessage}},
			{"timestamp only", []ColumnType{ColumnTypeTimestamp}},
			{"timestamp and message", []ColumnType{ColumnTypeTimestamp, ColumnTypeMessage}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				reader := NewRowReader(section)
				require.NoError(t, reader.SetProjectedColumns(tc.types, nil))
				require.NoError(t, reader.Open(context.Background()))
				t.Cleanup(func() { require.NoError(t, reader.Close()) })

				require.Len(t, readAllRecords(t, reader), 3)
			})
		}
	})

	t.Run("a stream match filters on the stream ID even when the projection omits it", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeMessage}, nil))
		require.NoError(t, reader.MatchStreams(slices.Values([]int64{2})))
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		got := readAllRecords(t, reader)
		require.Len(t, got, 2)

		var lines []string
		for _, rec := range got {
			lines = append(lines, string(rec.Line))
			require.Zero(t, rec.StreamID, "the stream ID is read for the match and not decoded")
		}
		require.ElementsMatch(t, []string{"line-2", "line-3"}, lines)
	})

	t.Run("a message predicate filters on the line even when the projection omits it", func(t *testing.T) {
		keep := func(line []byte) bool { return string(line) != "line-2" }

		for name, batch := range map[string]func(data []byte, offsets []int32, dstIndexes []int32) []int32{
			"with a batch form":    keepBatchOf(keep),
			"without a batch form": nil,
		} {
			t.Run(name, func(t *testing.T) {
				reader := NewRowReader(section)
				require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID}, nil))
				require.NoError(t, reader.MatchStreams(slices.Values([]int64{1, 2})))
				require.NoError(t, reader.SetPredicates([]RowPredicate{LogMessageFilterRowPredicate{Keep: keep, KeepBatch: batch}}))
				require.NoError(t, reader.Open(t.Context()))
				t.Cleanup(func() { require.NoError(t, reader.Close()) })

				got := readAllRecords(t, reader)
				require.Len(t, got, 2)
				for _, rec := range got {
					require.Empty(t, rec.Line)
				}
				require.Equal(t, int64(1), got[0].StreamID)
				require.Equal(t, int64(2), got[1].StreamID)
			})
		}
	})

	t.Run("a projection that selects nothing is rejected", func(t *testing.T) {
		err := NewRowReader(section).SetProjectedColumns(nil, nil)
		require.ErrorContains(t, err, "must select at least one column")
	})

	t.Run("a projection of only columns the section lacks fails on Open", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns(nil, []string{"not_in_this_section"}))
		require.ErrorContains(t, reader.Open(context.Background()), "none of the projected columns")
	})

	t.Run("the projection cannot be changed once reading has started", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })
		require.ErrorContains(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID}, nil), "cannot change projected columns")
	})
}

func TestRowReader_SetPredicates(t *testing.T) {
	section := buildTestProjectionSection(t)

	t.Run("a metadata predicate on a column the section does not hold is reduced against the empty value", func(t *testing.T) {
		// Every row of such a section reads as an empty value for the key, so the predicate
		// is reduced against "" rather than dropping the section outright.
		for _, tc := range []struct {
			name      string
			predicate RowPredicate
			wantRows  int
		}{
			{
				name:      "equality with a value matches nothing",
				predicate: MetadataMatcherRowPredicate{Key: "absent", Value: "v"},
				wantRows:  0,
			},
			{
				name:      "equality with the empty value matches every row",
				predicate: MetadataMatcherRowPredicate{Key: "absent", Value: ""},
				wantRows:  3,
			},
			{
				name: "a filter that accepts the empty value matches every row",
				predicate: MetadataFilterRowPredicate{Key: "absent", Keep: func(_, value string) bool {
					return value != "v"
				}},
				wantRows: 3,
			},
			{
				name: "a filter that rejects the empty value matches nothing",
				predicate: MetadataFilterRowPredicate{Key: "absent", Keep: func(_, value string) bool {
					return value == "v"
				}},
				wantRows: 0,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				reader := NewRowReader(section)
				require.NoError(t, reader.SetPredicates([]RowPredicate{tc.predicate}))
				require.NoError(t, reader.Open(context.Background()))
				t.Cleanup(func() { require.NoError(t, reader.Close()) })

				require.Len(t, readAllRecords(t, reader), tc.wantRows)
			})
		}
	})

	t.Run("a composite whose operands reduce to constants is folded away", func(t *testing.T) {
		// Those constants come from the reduction above. Folding must collapse them, so the
		// composite never reaches the dataset reader naming no column: that either fails the
		// read or, under a negation, panics.
		absentKeeps := MetadataMatcherRowPredicate{Key: "absentA", Value: ""}  // keeps every row
		absentDrops := MetadataMatcherRowPredicate{Key: "absentB", Value: "v"} // drops every row
		realEnvDev := MetadataMatcherRowPredicate{Key: "env", Value: "dev"}    // matches line-3

		for _, tc := range []struct {
			name      string
			predicate RowPredicate
			wantLines []string
		}{
			{"and of two keep-all constants", AndRowPredicate{Left: absentKeeps, Right: absentKeeps}, []string{"line-1", "line-2", "line-3"}},
			{"and of a keep-all constant and a real predicate", AndRowPredicate{Left: absentKeeps, Right: realEnvDev}, []string{"line-3"}},
			{"and of a drop-all constant and a real predicate", AndRowPredicate{Left: absentDrops, Right: realEnvDev}, nil},
			{"or of a drop-all constant and a real predicate", OrRowPredicate{Left: absentDrops, Right: realEnvDev}, []string{"line-3"}},
			{"or of a keep-all constant and a real predicate", OrRowPredicate{Left: absentKeeps, Right: realEnvDev}, []string{"line-1", "line-2", "line-3"}},
			{"not of a keep-all constant", NotRowPredicate{Inner: absentKeeps}, nil},
			{"not of a drop-all constant", NotRowPredicate{Inner: absentDrops}, []string{"line-1", "line-2", "line-3"}},
			{"not of a real predicate", NotRowPredicate{Inner: realEnvDev}, []string{"line-1", "line-2"}},
			{"nil branch folds like keep-all", AndRowPredicate{Left: nil, Right: realEnvDev}, []string{"line-3"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				reader := NewRowReader(section)
				require.NoError(t, reader.SetPredicates([]RowPredicate{tc.predicate}))
				require.NoError(t, reader.Open(context.Background()))
				t.Cleanup(func() { require.NoError(t, reader.Close()) })

				var lines []string
				for _, rec := range readAllRecords(t, reader) {
					lines = append(lines, string(rec.Line))
				}
				require.ElementsMatch(t, tc.wantLines, lines)
			})
		}
	})

	t.Run("a message predicate with a batch form returns the rows of its row form", func(t *testing.T) {
		var rowCalls, batchCalls int
		keep := func(line []byte) bool { return string(line) != "line-2" }
		predicate := LogMessageFilterRowPredicate{
			Keep: func(line []byte) bool {
				rowCalls++
				return keep(line)
			},
			KeepBatch: func(data []byte, offsets []int32, dstIndexes []int32) []int32 {
				batchCalls++
				return keepBatchOf(keep)(data, offsets, dstIndexes)
			},
		}

		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeMessage}, nil))
		require.NoError(t, reader.MatchStreams(slices.Values([]int64{1, 2})))
		require.NoError(t, reader.SetPredicates([]RowPredicate{predicate}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var lines []string
		for _, rec := range readAllRecords(t, reader) {
			lines = append(lines, string(rec.Line))
		}
		require.Equal(t, []string{"line-1", "line-3"}, lines)
		require.NotZero(t, batchCalls)
		require.Zero(t, rowCalls)
	})

	t.Run("a message predicate listed last runs only on the rows the earlier predicates keep", func(t *testing.T) {
		var seen []string
		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeMessage}, []string{"trace_id"}))
		require.NoError(t, reader.SetPredicates([]RowPredicate{
			TimeRangeRowPredicate{StartTime: unixSecond(15), EndTime: unixSecond(35), IncludeStart: true},
			MetadataMatcherRowPredicate{Key: "env", Value: "prod"},
			LogMessageFilterRowPredicate{Keep: func(line []byte) bool {
				seen = append(seen, string(line))
				return true
			}},
		}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		records := readAllRecords(t, reader)
		require.Len(t, records, 1)
		require.Equal(t, int64(2), records[0].StreamID)
		require.Equal(t, "line-2", string(records[0].Line))
		require.Equal(t, "t2", records[0].Metadata.Get("trace_id"))
		require.Equal(t, []string{"line-2"}, seen)
	})

	t.Run("a message predicate listed last runs only on the rows of the matched streams", func(t *testing.T) {
		var seen []string
		reader := NewRowReader(section)
		require.NoError(t, reader.MatchStreams(slices.Values([]int64{2})))
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeMessage}, nil))
		require.NoError(t, reader.SetPredicates([]RowPredicate{
			LogMessageFilterRowPredicate{Keep: func(line []byte) bool {
				seen = append(seen, string(line))
				return true
			}},
		}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		require.Len(t, readAllRecords(t, reader), 2)
		require.ElementsMatch(t, []string{"line-2", "line-3"}, seen)
	})

	t.Run("a message predicate listed first runs on every row, because the reader applies predicates in order", func(t *testing.T) {
		var seen []string
		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeMessage}, nil))
		require.NoError(t, reader.SetPredicates([]RowPredicate{
			LogMessageFilterRowPredicate{Keep: func(line []byte) bool {
				seen = append(seen, string(line))
				return true
			}},
			MetadataMatcherRowPredicate{Key: "env", Value: "prod"},
		}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		require.Len(t, readAllRecords(t, reader), 2)
		require.ElementsMatch(t, []string{"line-1", "line-2", "line-3"}, seen)
	})

	t.Run("a message predicate that drops every row ends the read with no record", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.SetPredicates([]RowPredicate{
			LogMessageFilterRowPredicate{Keep: func([]byte) bool { return false }},
		}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		require.Empty(t, readAllRecords(t, reader))
	})

	t.Run("a message predicate sees an empty line and a line that is not UTF-8 byte for byte", func(t *testing.T) {
		notUTF8 := []byte{0xff, 0xfe, 'a'}
		lineSection := buildSectionFromRecords(t, SortStreamASC, []Record{
			{StreamID: 1, Timestamp: unixSecond(10), Line: []byte{}},
			{StreamID: 1, Timestamp: unixSecond(20), Line: notUTF8},
			{StreamID: 1, Timestamp: unixSecond(30), Line: []byte("plain")},
		})

		var seen [][]byte
		reader := NewRowReader(lineSection)
		require.NoError(t, reader.SetPredicates([]RowPredicate{
			LogMessageFilterRowPredicate{Keep: func(line []byte) bool {
				seen = append(seen, slices.Clone(line))
				return len(line) == 0 || string(line) == "plain"
			}},
		}))
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		var kept []string
		for _, record := range readAllRecords(t, reader) {
			kept = append(kept, string(record.Line))
		}
		require.ElementsMatch(t, []string{"", "plain"}, kept)
		require.ElementsMatch(t, [][]byte{{}, notUTF8, []byte("plain")}, seen)
	})

	t.Run("predicates cannot be changed once reading has started", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })
		require.ErrorContains(t, reader.SetPredicates(nil), "cannot change predicate")
	})
}

func TestTranslateLogsPredicate(t *testing.T) {
	t.Run("a message predicate reads a nil message value as an empty line", func(t *testing.T) {
		section := buildTestProjectionSection(t)
		dset, err := section.makeDataset()
		require.NoError(t, err)

		var seen [][]byte
		predicate := translateLogsPredicate(
			LogMessageFilterRowPredicate{Keep: func(line []byte) bool {
				seen = append(seen, line)
				return len(line) == 0
			}},
			dset.Columns(), section.Columns(),
		)

		funcPredicate, ok := predicate.(dataset.FuncPredicate)
		require.True(t, ok, "want a function predicate, got %T", predicate)
		var keep bool
		require.NotPanics(t, func() { keep = funcPredicate.Keep(funcPredicate.Column, dataset.Value{}) })
		require.True(t, keep)
		require.Len(t, seen, 1)
		require.Empty(t, seen[0])
	})

	t.Run("a message predicate passes the lines of an array to its batch form", func(t *testing.T) {
		section := buildTestProjectionSection(t)
		dset, err := section.makeDataset()
		require.NoError(t, err)

		predicate := translateLogsPredicate(
			LogMessageFilterRowPredicate{
				Keep:      func(line []byte) bool { return len(line) > 0 },
				KeepBatch: keepBatchOf(func(line []byte) bool { return len(line) > 0 }),
			},
			dset.Columns(), section.Columns(),
		)

		funcPredicate, ok := predicate.(dataset.FuncPredicate)
		require.True(t, ok, "want a function predicate, got %T", predicate)
		require.NotNil(t, funcPredicate.KeepBatch)

		values := columnar.NewUTF8([]byte("abcde"), []int32{0, 3, 3, 5}, memory.Bitmap{})
		require.Equal(t, []int32{0, 2}, funcPredicate.KeepBatch(funcPredicate.Column, values, nil))
	})

	t.Run("a message predicate without a batch form translates to a predicate without one", func(t *testing.T) {
		section := buildTestProjectionSection(t)
		dset, err := section.makeDataset()
		require.NoError(t, err)

		predicate := translateLogsPredicate(
			LogMessageFilterRowPredicate{Keep: func([]byte) bool { return true }},
			dset.Columns(), section.Columns(),
		)

		funcPredicate, ok := predicate.(dataset.FuncPredicate)
		require.True(t, ok, "want a function predicate, got %T", predicate)
		require.Nil(t, funcPredicate.KeepBatch)
	})

	t.Run("a message predicate on a section with no message column reduces against the empty line", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			keep     func(line []byte) bool
			wantKeep bool
		}{
			{"a filter that accepts the empty line keeps every row", func(line []byte) bool { return string(line) != "x" }, true},
			{"a filter that rejects the empty line drops every row", func(line []byte) bool { return string(line) == "x" }, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				predicate := translateLogsPredicate(LogMessageFilterRowPredicate{Keep: tc.keep}, nil, nil)

				keep, isConst := dataset.IsConstPredicate(predicate)
				require.True(t, isConst, "want a constant predicate, got %T", predicate)
				require.Equal(t, tc.wantKeep, keep)
			})
		}
	})
}

func TestRowReader_Reset(t *testing.T) {
	section := buildTestProjectionSection(t)

	t.Run("the column projection is cleared, so every column is read again", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeStreamID}, nil))
		reader.Reset(section)
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		got := readAllRecords(t, reader)
		require.Len(t, got, 3)
		require.Equal(t, "line-1", string(got[0].Line))
		require.Equal(t, `{env="prod", trace_id="t1"}`, got[0].Metadata.String())
	})

	t.Run("the predicates are cleared, so no row is filtered out", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.SetPredicates([]RowPredicate{
			MetadataMatcherRowPredicate{Key: "env", Value: "dev"},
		}))
		reader.Reset(section)
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		require.Len(t, readAllRecords(t, reader), 3)
	})

	t.Run("the stream match is cleared, so every stream is read again", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.MatchStreams(slices.Values([]int64{1})))
		reader.Reset(section)
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })

		require.Len(t, readAllRecords(t, reader), 3)
	})
}

func TestRowReader_StagedDownloads(t *testing.T) {
	matched := []int64{1, 5, 15, 22, 25, 35}

	large := stagedTestRecords(40, 300, 1<<10)
	largeSection := buildSectionWithOptions(t, BuilderOptions{StripeMergeLimit: 2, SortOrder: SortStreamASC, AppendStrategy: AppendOrdered, PageSizeHint: 256 << 10, PageMaxRowCount: 2000}, large)
	largeMessageBytes := messageColumnBytes(t, largeSection)

	bigRecords := bigMetadataRecords(4200, 1<<10, 100)
	bigSection := buildSectionWithOptions(t, BuilderOptions{StripeMergeLimit: 2, SortOrder: SortStreamASC, AppendStrategy: AppendOrdered, PageSizeHint: 64 << 10}, bigRecords)

	t.Run("downloads on the first read only the pages of the rows that pass the stream and metadata predicates", func(t *testing.T) {
		got := readStaged(t, largeSection, func(r *RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values(matched)))
			require.NoError(t, r.SetPredicates([]RowPredicate{MetadataMatcherRowPredicate{Key: "kind", Value: "a"}}))
		})

		want := linesWhere(large, func(r Record) bool {
			return slices.Contains(matched, r.StreamID) && r.Metadata.Get("kind") == "a"
		})
		require.Equal(t, want, sortedLines(got.records))
		require.Zero(t, got.openedBytes)
		require.Less(t, got.readBytes*2, largeMessageBytes)
		require.Equal(t, got.firstReadBytes, got.readBytes)
	})

	t.Run("downloads no page of the other columns when no row passes the metadata predicate", func(t *testing.T) {
		got := readStaged(t, largeSection, func(r *RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values(matched)))
			require.NoError(t, r.SetPredicates([]RowPredicate{MetadataMatcherRowPredicate{Key: "kind", Value: "b"}}))
		})

		require.Empty(t, got.records)
		require.Zero(t, got.openedBytes)
		require.Zero(t, got.readBytes)
	})

	t.Run("downloads every page of the other columns, and counts each row once, when every row passes", func(t *testing.T) {
		streams := make([]int64, 0, 40)
		for id := int64(1); id <= 40; id++ {
			streams = append(streams, id)
		}

		got := readStaged(t, largeSection, func(r *RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values(streams)))
		})

		require.Len(t, got.records, len(large))
		require.Zero(t, got.openedBytes)
		require.GreaterOrEqual(t, got.readBytes, largeMessageBytes)
		require.Equal(t, got.firstReadBytes, got.readBytes)
		require.Equal(t, int64(len(large)), got.primaryRows)
	})

	t.Run("downloads every page at open when a predicate reads the log line", func(t *testing.T) {
		wantLine := bigRecords[3000].Line
		lineFilter := LogMessageFilterRowPredicate{Keep: func(line []byte) bool { return bytes.Equal(line, wantLine) }}

		staged := readStaged(t, bigSection, func(r *RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1})))
		})
		require.Zero(t, staged.openedBytes, "the section must stay above the staging threshold, so grow its records if the threshold grew")

		got := readStaged(t, bigSection, func(r *RowReader) {
			require.NoError(t, r.MatchStreams(slices.Values([]int64{1})))
			require.NoError(t, r.SetPredicates([]RowPredicate{
				OrRowPredicate{Left: lineFilter, Right: lineFilter},
				TimeRangeRowPredicate{StartTime: time.Unix(0, 0), EndTime: time.Unix(4_000_000_000, 0), IncludeStart: true, IncludeEnd: true},
			}))
		})

		require.Len(t, got.records, 1)
		require.NotZero(t, got.openedBytes)
		require.Equal(t, got.openedBytes, got.readBytes)
	})

	t.Run("downloads every page at open when the predicate column holds most of the bytes", func(t *testing.T) {
		trace := bigRecords[3000].Metadata.Get("trace")

		got := readStaged(t, bigSection, func(r *RowReader) {
			require.NoError(t, r.SetPredicates([]RowPredicate{MetadataMatcherRowPredicate{Key: "trace", Value: trace}}))
		})

		require.Len(t, got.records, 1)
		require.GreaterOrEqual(t, got.openedBytes, messageColumnBytes(t, bigSection))
		require.Equal(t, got.openedBytes, got.readBytes)
	})
}

// keepBatchOf returns a KeepBatch function that applies keep to each line.
func keepBatchOf(keep func(line []byte) bool) func(data []byte, offsets []int32, dstIndexes []int32) []int32 {
	return func(data []byte, offsets []int32, dstIndexes []int32) []int32 {
		for i := range len(offsets) - 1 {
			if keep(data[offsets[i]:offsets[i+1]]) {
				dstIndexes = append(dstIndexes, int32(i))
			}
		}
		return dstIndexes
	}
}

func unixSecond(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// readAllRecords drains reader into a slice, copying each record because the reader reuses
// its decode buffers across reads.
func readAllRecords(t *testing.T, reader *RowReader) []Record {
	t.Helper()

	var out []Record
	buf := make([]Record, 2)
	for {
		n, err := reader.Read(context.Background(), buf)
		for i := range buf[:n] {
			out = append(out, buf[i].Copy())
		}
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
	}
}

// buildTestProjectionSection returns a section holding three records across two streams, with
// two metadata keys and distinct lines and timestamps, so a test can tell which columns a
// read actually decoded.
func buildTestProjectionSection(t *testing.T) *Section {
	logsBuilder := NewBuilder(nil, BuilderOptions{
		StripeMergeLimit: 2,
		SortOrder:        SortStreamASC,
	})
	logsBuilder.Append(Record{
		StreamID:  1,
		Timestamp: unixSecond(10),
		Metadata:  labels.FromStrings("trace_id", "t1", "env", "prod"),
		Line:      []byte("line-1"),
	})
	logsBuilder.Append(Record{
		StreamID:  2,
		Timestamp: unixSecond(20),
		Metadata:  labels.FromStrings("trace_id", "t2", "env", "prod"),
		Line:      []byte("line-2"),
	})
	logsBuilder.Append(Record{
		StreamID:  2,
		Timestamp: unixSecond(30),
		Metadata:  labels.FromStrings("trace_id", "t3", "env", "dev"),
		Line:      []byte("line-3"),
	})

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(logsBuilder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	var section *Section
	for _, sec := range obj.Sections() {
		section, err = Open(context.Background(), sec)
		require.NoError(t, err)
	}
	return section
}

func buildTestSection(t *testing.T) *Section {
	return buildSectionFromRecords(t, SortStreamASC, []Record{
		{
			StreamID:  1,
			Timestamp: time.Now(),
			Line:      []byte("test"),
		},
		{
			StreamID:  2,
			Timestamp: time.Now(),
			Line:      []byte("test2"),
		},
	})
}

func buildSectionFromRecords(t *testing.T, sortOrder SortOrder, records []Record) *Section {
	return buildSectionWithOptions(t, BuilderOptions{StripeMergeLimit: 2, SortOrder: sortOrder}, records)
}

func buildSectionWithOptions(t *testing.T, opts BuilderOptions, records []Record) *Section {
	logsBuilder := NewBuilder(nil, opts)
	for _, record := range records {
		logsBuilder.Append(record)
	}

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(logsBuilder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	var logsSection *Section
	for _, section := range obj.Sections() {
		logsSection, err = Open(t.Context(), section)
		require.NoError(t, err)
	}
	return logsSection
}

// stagedTestRecords returns rowsPerStream records for each stream from 1 to streams. The lines hold
// random bytes, so compression keeps their size. The metadata key "kind" alternates between "a" and
// "c".
func stagedTestRecords(streams, rowsPerStream, lineSize int) []Record {
	rng := rand.New(rand.NewSource(1))
	start := time.Unix(1_700_000_000, 0)

	records := make([]Record, 0, streams*rowsPerStream)
	for stream := 1; stream <= streams; stream++ {
		for row := range rowsPerStream {
			line := make([]byte, lineSize)
			_, _ = rng.Read(line)

			kind := "a"
			if row%2 == 1 {
				kind = "c"
			}
			records = append(records, Record{
				StreamID:  int64(stream),
				Timestamp: start.Add(-time.Duration(row) * time.Second),
				Metadata:  labels.FromStrings("kind", kind),
				Line:      line,
			})
		}
	}
	return records
}

// stagedRead is what readStaged measures.
type stagedRead struct {
	records        []Record
	openedBytes    int64 // Compressed bytes of secondary pages downloaded when Open returned.
	firstReadBytes int64 // Compressed bytes of secondary pages downloaded when the first Read returned.
	readBytes      int64 // Compressed bytes of secondary pages downloaded when the read ended.
	primaryRows    int64 // Value of the primary.rows.read statistic.
}

// bigMetadataRecords returns one stream of rows records. Each record has a random "trace" metadata
// value of traceSize bytes and a random line of lineSize bytes, so the metadata column holds most
// of the bytes of the section. The timestamps descend.
func bigMetadataRecords(rows, traceSize, lineSize int) []Record {
	rng := rand.New(rand.NewSource(2))
	start := time.Unix(1_700_000_000, 0)

	records := make([]Record, 0, rows)
	for row := range rows {
		trace := make([]byte, traceSize)
		_, _ = rng.Read(trace)
		line := make([]byte, lineSize)
		_, _ = rng.Read(line)

		records = append(records, Record{
			StreamID:  1,
			Timestamp: start.Add(-time.Duration(row) * time.Second),
			Metadata:  labels.FromStrings("trace", string(trace)),
			Line:      line,
		})
	}
	return records
}

// readStaged reads every row of a reader that configure sets up.
func readStaged(t *testing.T, section *Section, configure func(*RowReader)) stagedRead {
	t.Helper()

	ctx, _ := xcap.NewCapture(t.Context(), nil)
	ctx, region := xcap.StartRegion(ctx, RegionRead)
	defer region.End()

	reader := NewRowReader(section)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	configure(reader)
	require.NoError(t, reader.Open(ctx))

	got := stagedRead{openedBytes: observedInt64(region, dataobj.StatDatasetSecondaryColumnBytes.Name())}
	buf := make([]Record, 64)
	first := true
	for {
		n, err := reader.Read(ctx, buf)
		for i := range buf[:n] {
			got.records = append(got.records, buf[i].Copy())
		}
		if first {
			got.firstReadBytes = observedInt64(region, dataobj.StatDatasetSecondaryColumnBytes.Name())
			first = false
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	got.readBytes = observedInt64(region, dataobj.StatDatasetSecondaryColumnBytes.Name())
	got.primaryRows = observedInt64(region, dataobj.StatDatasetPrimaryRowsRead.Name())
	return got
}

// observedInt64 returns the value that region holds for the statistic called name, or zero.
func observedInt64(region *xcap.Region, name string) int64 {
	for _, obs := range region.Observations() {
		if obs.Statistic.Name() == name {
			return obs.Value.(int64)
		}
	}
	return 0
}

// messageColumnBytes returns the compressed size of the message column of section.
func messageColumnBytes(t *testing.T, section *Section) int64 {
	t.Helper()

	dset, err := section.makeDataset()
	require.NoError(t, err)
	for i, col := range section.Columns() {
		if col.Type == ColumnTypeMessage {
			return int64(dset.Columns()[i].ColumnDesc().CompressedSize)
		}
	}
	t.Fatal("section has no message column")
	return 0
}

// linesWhere returns the sorted lines of the records that keep accepts.
func linesWhere(records []Record, keep func(Record) bool) []string {
	var lines []string
	for _, record := range records {
		if keep(record) {
			lines = append(lines, string(record.Line))
		}
	}
	slices.Sort(lines)
	return lines
}

// sortedLines returns the sorted lines of records.
func sortedLines(records []Record) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, string(record.Line))
	}
	slices.Sort(lines)
	return lines
}
