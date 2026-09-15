package sortmerge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/scratch"
)

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
			_, err := result.Collect(MixedRunIterator(context.Background(), []Run{test.run}, test.schema))
			require.ErrorContains(t, err, test.want)
		})
	}
	t.Run("early stop does not open successor", func(t *testing.T) {
		for res := range MixedRunIterator(context.Background(), []Run{{valid, {}}}, nil) {
			_, err := res.Value()
			require.NoError(t, err)
			break
		}
	})
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

func TestRunSequence_ReleasesSections(t *testing.T) {
	sec := buildRunSection(t, logs.Record{StreamID: 1, Timestamp: time.Unix(1, 0)})
	input := RemappedSection{Section: sec, Remap: map[int64]int64{1: 1}}
	s := &runSequence{ctx: context.Background(), remaining: Run{input, input}, bufferSize: 1}
	require.Nil(t, s.current)
	require.True(t, s.Next())
	first := s.current
	require.True(t, s.Next())
	require.NotSame(t, first, s.current, "successor must replace the active reader")
	require.False(t, s.Next())
	require.Nil(t, s.current, "exhaustion must release the last reader and its row buffers")
	s.Close()

	s = &runSequence{ctx: context.Background(), remaining: Run{input, {}}, bufferSize: 1}
	require.True(t, s.Next())
	require.True(t, s.Next())
	_, err := s.At().Value()
	require.Error(t, err)
	require.Nil(t, s.current, "a failed successor must not retain the preceding reader")
	s.Close()
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
