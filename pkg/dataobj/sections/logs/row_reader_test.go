package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
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
	logsSection := buildTestSection(t)

	readBuf := make([]Record, 3)
	rowReader := NewRowReader(logsSection)

	err := rowReader.MatchStreams(slices.Values([]int64{1}))
	require.NoError(t, err)
	require.NoError(t, rowReader.Open(context.Background()))
	n, err := rowReader.Read(context.Background(), readBuf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
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

	t.Run("a column a predicate needs is read even when it is not projected", func(t *testing.T) {
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

	t.Run("a stream match adds the stream ID column back to the projection", func(t *testing.T) {
		// The projection omits the stream ID, so the match can only filter if the widening
		// adds that column back. The widened column is decoded into the Record too.
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
			require.Equal(t, int64(2), rec.StreamID, "the widened stream ID column must be decoded")
		}
		require.ElementsMatch(t, []string{"line-2", "line-3"}, lines)
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

	t.Run("predicates cannot be changed once reading has started", func(t *testing.T) {
		reader := NewRowReader(section)
		require.NoError(t, reader.Open(context.Background()))
		t.Cleanup(func() { require.NoError(t, reader.Close()) })
		require.ErrorContains(t, reader.SetPredicates(nil), "cannot change predicate")
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

// unknownRowPredicate is a RowPredicate the reader does not know about, standing in for one
// added later without teaching projectPredicateColumns about its columns.
type unknownRowPredicate struct{}

func (unknownRowPredicate) isRowPredicate() {}

func TestProjectPredicateColumns(t *testing.T) {
	// Distinct leaves on each side, so a branch that is not walked shows up as a missing
	// column rather than being masked by its sibling.
	var (
		timeLeaf     = TimeRangeRowPredicate{}
		messageLeaf  = LogMessageFilterRowPredicate{Keep: func([]byte) bool { return true }}
		matcherLeaf  = MetadataMatcherRowPredicate{Key: "matched"}
		filterLeaf   = MetadataFilterRowPredicate{Key: "filtered", Keep: func(_, _ string) bool { return true }}
		bothColumns  = map[ColumnType]struct{}{ColumnTypeTimestamp: {}, ColumnTypeMessage: {}}
		bothMetadata = map[string]struct{}{"matched": {}, "filtered": {}}
	)

	for _, tc := range []struct {
		name         string
		predicate    RowPredicate
		wantTypes    map[ColumnType]struct{}
		wantMetadata map[string]struct{}
	}{
		{"and walks both branches", AndRowPredicate{Left: timeLeaf, Right: messageLeaf}, bothColumns, map[string]struct{}{}},
		{"or walks both branches", OrRowPredicate{Left: timeLeaf, Right: messageLeaf}, bothColumns, map[string]struct{}{}},
		{"not walks its inner predicate", NotRowPredicate{Inner: timeLeaf}, map[ColumnType]struct{}{ColumnTypeTimestamp: {}}, map[string]struct{}{}},
		{"both metadata predicate kinds name their key", AndRowPredicate{Left: matcherLeaf, Right: filterLeaf}, map[ColumnType]struct{}{}, bothMetadata},
		{"nesting recurses all the way down", NotRowPredicate{Inner: OrRowPredicate{Left: filterLeaf, Right: AndRowPredicate{Left: timeLeaf, Right: matcherLeaf}}}, map[ColumnType]struct{}{ColumnTypeTimestamp: {}}, bothMetadata},
		{"a nil branch names nothing", AndRowPredicate{Left: nil, Right: timeLeaf}, map[ColumnType]struct{}{ColumnTypeTimestamp: {}}, map[string]struct{}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			types, names := map[ColumnType]struct{}{}, map[string]struct{}{}
			projectPredicateColumns(tc.predicate, types, names)
			require.Equal(t, tc.wantTypes, types)
			require.Equal(t, tc.wantMetadata, names)
		})
	}

	t.Run("an unknown predicate panics", func(t *testing.T) {
		// The contract is that it fails loudly, not that it uses any particular wording.
		var recovered any
		require.Panics(t, func() {
			defer func() { recovered = recover(); panic(recovered) }()
			projectPredicateColumns(unknownRowPredicate{}, map[ColumnType]struct{}{}, map[string]struct{}{})
		})
		require.Contains(t, fmt.Sprint(recovered), "unknownRowPredicate")
	})
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
	logsBuilder := NewBuilder(nil, BuilderOptions{
		StripeMergeLimit: 2,
		SortOrder:        SortStreamASC,
	})
	logsBuilder.Append(Record{
		StreamID:  1,
		Timestamp: time.Now(),
		Line:      []byte("test"),
	})
	logsBuilder.Append(Record{
		StreamID:  2,
		Timestamp: time.Now(),
		Line:      []byte("test2"),
	})

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(logsBuilder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	var logsSection *Section
	for _, section := range obj.Sections() {
		logsSection, err = Open(context.Background(), section)
		require.NoError(t, err)
	}
	return logsSection
}
