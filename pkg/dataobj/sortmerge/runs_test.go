package sortmerge

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/scratch"
)

type observedSectionIterator struct {
	nextCalls  int
	closeCalls int
	onNext     func()
	err        error
	rows       []int64
}

func (s *observedSectionIterator) Next() bool {
	s.nextCalls++
	if s.onNext != nil {
		s.onNext()
	}
	if s.err != nil {
		return s.nextCalls == 1
	}
	return s.nextCalls <= len(s.rows)
}

func (s *observedSectionIterator) At() result.Result[dataset.Row] {
	if s.err != nil {
		return result.Error[dataset.Row](s.err)
	}
	return result.Value(dataset.Row{Values: []dataset.Value{dataset.Int64Value(1), dataset.Int64Value(s.rows[s.nextCalls-1])}})
}

func (s *observedSectionIterator) Columns() []*logs.Column {
	return nil
}

func (s *observedSectionIterator) Close() {
	s.closeCalls++
}

func buildRunSection(t testing.TB, records ...logs.Record) *dataobj.Section {
	t.Helper()
	b := logs.NewBuilder(nil, logs.BuilderOptions{PageSizeHint: 2048, BufferSize: 2048, StripeMergeLimit: 2, SortOrder: logs.SortStreamASC})
	for _, record := range records {
		b.Append(record)
	}
	objBuilder := dataobj.NewBuilder(scratch.NewMemory())
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	require.Len(t, obj.Sections(), 1)
	return obj.Sections()[0]
}

func TestMixedRunIterator_SectionTransitions(t *testing.T) {
	section := func(id, ts int64, metadata string) RemappedSection {
		return RemappedSection{
			Section: buildRunSection(t, logs.Record{StreamID: id, Timestamp: time.Unix(ts, 0), Line: []byte(metadata), Metadata: labels.FromStrings(metadata, "value")}),
			Remap:   map[int64]int64{id: 1},
		}
	}
	// The same global stream spans sections, using different local IDs and columns.
	runs := []Run{
		{section(7, 4, "a"), section(8, 2, "b")},
		{section(9, 3, "c"), section(7, 2, "d"), section(1, 1, "e")},
		nil,
	}
	var timestamps []int64
	metadata := map[string]string{}
	for res := range MixedRunIterator(context.Background(), runs, nil) {
		record, err := res.Value()
		require.NoError(t, err)
		require.Equal(t, int64(1), record.StreamID)
		timestamps = append(timestamps, record.Timestamp.Unix())
		metadata[string(record.Line)] = record.Metadata.Get(string(record.Line))
	}
	require.Equal(t, []int64{4, 3, 2, 2, 1}, timestamps)
	require.Equal(t, map[string]string{"a": "value", "b": "value", "c": "value", "d": "value", "e": "value"}, metadata)
}

func TestMixedRunIterator_Errors(t *testing.T) {
	sec := buildRunSection(t, logs.Record{StreamID: 1, Timestamp: time.Unix(1, 0)})
	valid := RemappedSection{Section: sec, Remap: map[int64]int64{1: 1}}
	later := RemappedSection{Section: buildRunSection(t, logs.Record{StreamID: 1, Timestamp: time.Unix(2, 0)}), Remap: valid.Remap}
	tests := []struct {
		name   string
		run    Run
		schema []string
		want   string
	}{
		{name: "later section fails", run: Run{valid, {}}, want: "section and stream remap are required"},
		{name: "missing stream", run: Run{{Section: sec, Remap: map[int64]int64{}}}, want: "absent from stream remap"},
		{name: "schema mismatch", run: Run{valid}, schema: []string{"label:app"}, want: "does not match expected sort schema"},
		{name: "descending stream at boundary", run: Run{{Section: sec, Remap: map[int64]int64{1: 2}}, valid}, want: "run is not sorted"},
		{name: "ascending timestamp at boundary", run: Run{valid, later}, want: "run is not sorted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errorsSeen := 0
			for res := range MixedRunIterator(context.Background(), []Run{test.run}, test.schema) {
				require.Zero(t, errorsSeen, "no result may follow the terminal error")
				_, err := res.Value()
				if err != nil {
					require.ErrorContains(t, err, test.want)
					errorsSeen++
				}
			}
			require.Equal(t, 1, errorsSeen)
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := result.Collect(MixedRunIterator(ctx, []Run{{valid}}, nil))
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("cancel during iteration", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		count := 0
		for res := range MixedRunIterator(ctx, []Run{{valid, valid}}, nil) {
			_, err := res.Value()
			if count == 0 {
				require.NoError(t, err)
				cancel()
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			count++
		}
		require.Equal(t, 2, count)
	})
}

func TestRunSequence(t *testing.T) {
	setup := func(t *testing.T) (*runSequence, *observedSectionIterator, *observedSectionIterator) {
		t.Helper()
		first := &observedSectionIterator{rows: []int64{4, 3}}
		second := &observedSectionIterator{rows: []int64{2, 1}}
		s := &runSequence{ctx: t.Context(), remaining: []sectionIterator{first, second}}
		t.Cleanup(s.Close)
		return s, first, second
	}
	readRowTimestamp := func(t *testing.T, s *runSequence) int64 {
		t.Helper()
		require.True(t, s.Next())
		row, err := s.At().Value()
		require.NoError(t, err)
		return row.Values[1].Int64()
	}
	assertTerminalError := func(t *testing.T, s *runSequence, want error) {
		t.Helper()
		require.True(t, s.Next())
		_, err := s.At().Value()
		require.ErrorIs(t, err, want)
		require.False(t, s.Next())
		s.Close()
		require.Nil(t, s.current)
	}

	t.Run("happy path closes before transitions", func(t *testing.T) {
		s, first, second := setup(t)
		second.onNext = func() { require.Equal(t, 1, first.closeCalls) }
		// Read all values from first iterator
		require.Equal(t, int64(4), readRowTimestamp(t, s))
		require.Equal(t, int64(3), readRowTimestamp(t, s))
		require.Equal(t, 0, first.closeCalls)
		require.Equal(t, 0, second.closeCalls)

		// Read first value from second iterator, triggering close of first.
		require.Equal(t, int64(2), readRowTimestamp(t, s))
		require.Equal(t, 1, first.closeCalls)

		// Finish reading second iterator
		require.Equal(t, int64(1), readRowTimestamp(t, s))
		require.Equal(t, 0, second.closeCalls)

		// Call Next() a final time to trigger close of second section
		s.Next()
		require.Equal(t, 1, second.closeCalls)

		s.Close()
		require.Equal(t, 3, first.nextCalls)
		require.Equal(t, 3, second.nextCalls)
	})
	t.Run("close before init", func(t *testing.T) {
		s, first, second := setup(t)
		s.Close()
		require.False(t, s.Next())
		require.Zero(t, first.nextCalls)
		require.Zero(t, second.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Equal(t, 1, second.closeCalls)
	})
	t.Run("first section opening error", func(t *testing.T) {
		s, first, second := setup(t)
		first.err = errors.New("opening first section failed")
		assertTerminalError(t, s, first.err)
		require.Equal(t, 1, first.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Zero(t, second.nextCalls)
		require.Equal(t, 1, second.closeCalls)
	})
	t.Run("second section opening error closes first section", func(t *testing.T) {
		s, first, second := setup(t)
		third := &observedSectionIterator{rows: []int64{0}}
		s.remaining = append(s.remaining, third)
		second.err = errors.New("opening second section failed")
		require.Equal(t, int64(4), readRowTimestamp(t, s))
		require.Equal(t, int64(3), readRowTimestamp(t, s))
		assertTerminalError(t, s, second.err)
		require.Equal(t, 3, first.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Equal(t, 1, second.nextCalls)
		require.Equal(t, 1, second.closeCalls)
		require.Zero(t, third.nextCalls)
		require.Equal(t, 1, third.closeCalls)
	})
	t.Run("empty section", func(t *testing.T) {
		s, first, second := setup(t)
		first.rows = nil
		require.Equal(t, int64(2), readRowTimestamp(t, s))
		require.Equal(t, int64(1), readRowTimestamp(t, s))
		require.False(t, s.Next())
		require.Equal(t, 1, first.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Equal(t, 1, second.closeCalls)
	})
	t.Run("early stop", func(t *testing.T) {
		s, first, second := setup(t)
		require.Equal(t, int64(4), readRowTimestamp(t, s))
		s.Close()
		require.False(t, s.Next())
		require.Equal(t, 1, first.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Zero(t, second.nextCalls)
		require.Equal(t, 1, second.closeCalls)
	})
	t.Run("cancel before start", func(t *testing.T) {
		s, first, second := setup(t)
		ctx, cancel := context.WithCancel(t.Context())
		s.ctx = ctx
		cancel()
		assertTerminalError(t, s, context.Canceled)
		require.Zero(t, first.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Zero(t, second.nextCalls)
		require.Equal(t, 1, second.closeCalls)
	})
	t.Run("cancel after row", func(t *testing.T) {
		s, first, second := setup(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s.ctx = ctx
		require.Equal(t, int64(4), readRowTimestamp(t, s))
		cancel()
		assertTerminalError(t, s, context.Canceled)
		require.Equal(t, 1, first.nextCalls)
		require.Equal(t, 1, first.closeCalls)
		require.Zero(t, second.nextCalls)
		require.Equal(t, 1, second.closeCalls)
	})
}

func TestLazySectionIterator(t *testing.T) {
	sec := buildRunSection(t,
		logs.Record{StreamID: 1, Timestamp: time.Unix(2, 0)},
		logs.Record{StreamID: 1, Timestamp: time.Unix(1, 0)},
	)
	setup := func(t *testing.T) *lazySectionIterator {
		t.Helper()
		s := &lazySectionIterator{ctx: t.Context(), input: RemappedSection{Section: sec, Remap: map[int64]int64{1: 7}}, bufferSize: 1}
		t.Cleanup(s.Close)
		require.Nil(t, s.sequence, "construction must not open the reader")
		return s
	}
	t.Run("happy path", func(t *testing.T) {
		s := setup(t)
		for _, ts := range []int64{2, 1} {
			require.True(t, s.Next())
			row, err := s.At().Value()
			require.NoError(t, err)
			require.Equal(t, int64(7), row.Values[0].Int64())
			require.Equal(t, time.Unix(ts, 0).UnixNano(), row.Values[1].Int64())
		}
		require.False(t, s.Next())
		s.Close()
		require.Nil(t, s.sequence)
	})
	t.Run("close before init", func(t *testing.T) {
		s := setup(t)
		s.Close()
		require.Nil(t, s.sequence)
	})
	t.Run("opening failure", func(t *testing.T) {
		s := setup(t)
		s.input = RemappedSection{}
		require.True(t, s.Next())
		_, err := s.At().Value()
		require.ErrorContains(t, err, "section and stream remap are required")
		require.False(t, s.Next())
		s.Close()
		require.Nil(t, s.sequence)
	})
	t.Run("row failure", func(t *testing.T) {
		s := setup(t)
		s.input.Remap = map[int64]int64{}
		require.True(t, s.Next())
		_, err := s.At().Value()
		require.ErrorContains(t, err, "absent from stream remap")
		require.False(t, s.Next())
		s.Close()
		require.Nil(t, s.sequence)
	})
}

func TestRunSequence_TerminalStickiness(t *testing.T) {
	first := &observedSectionIterator{err: errors.New("section failed")}
	successor := &observedSectionIterator{rows: []int64{1}}
	s := &runSequence{ctx: t.Context(), remaining: []sectionIterator{first, successor}}
	t.Cleanup(s.Close)

	require.True(t, s.Next())
	_, err := s.At().Value()
	require.ErrorIs(t, err, first.err)
	for range 3 {
		require.False(t, s.Next(), "the terminal error must not be delivered again")
	}
	for range 3 {
		s.Close()
		require.False(t, s.Next())
	}
	require.Equal(t, 1, first.nextCalls)
	require.Equal(t, 1, first.closeCalls)
	require.Zero(t, successor.nextCalls)
	require.Equal(t, 1, successor.closeCalls)
	require.Nil(t, s.current)
}

// Measuring initialization through the first record isolates active reader
// allocations from cumulative allocations for processing all sections.
func BenchmarkMixedRunIteratorFirstRecord(b *testing.B) {
	sec := buildRunSection(b, logs.Record{StreamID: 1, Timestamp: time.Unix(1, 0)})
	for _, count := range []int{1, 16, 128} {
		b.Run(fmt.Sprintf("runs=4/sections=%d", count), func(b *testing.B) {
			runs := make([]Run, 4)
			for i := range runs {
				for range count {
					runs[i] = append(runs[i], RemappedSection{Section: sec, Remap: map[int64]int64{1: 1}})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for res := range MixedRunIterator(context.Background(), runs, nil) {
					_, err := res.Value()
					require.NoError(b, err)
					break
				}
			}
		})
	}
}
