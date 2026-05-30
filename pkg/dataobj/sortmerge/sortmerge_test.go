package sortmerge_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = logsobj.BuilderConfig{
	BuilderBaseConfig: logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 20, // 1 MiB
		TargetSectionSize:       8 << 10, // 8 KiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	},
}

const testTenant = "test"

// buildObject builds a single in-memory dataobj containing one stream with
// lineCount entries spaced 1s apart starting at base.
func buildObject(t *testing.T, labels string, base time.Time, lineCount int) (*dataobj.Object, func()) {
	t.Helper()

	b, err := logsobj.NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	entries := make([]push.Entry, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		entries = append(entries, push.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second).UTC(),
			Line:      "line",
		})
	}
	require.NoError(t, b.Append(testTenant, logproto.Stream{
		Labels:  labels,
		Entries: entries,
	}))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	return obj, func() { _ = closer.Close() }
}

func collectSections(t *testing.T, obj *dataobj.Object) []*dataobj.Section {
	t.Helper()
	var out []*dataobj.Section
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return logs.CheckSection(s) && s.Tenant == testTenant
	}) {
		out = append(out, sec)
	}
	require.NotEmpty(t, out, "expected at least one logs section for tenant")
	return out
}

// TestIterator_MergesAndOrdersByTimestamp builds two single-stream data
// objects whose timestamp ranges interleave, then verifies that
// sortmerge.Iterator merges them into a single non-increasing-timestamp
// stream (SortTimestampDESC) and emits every input record exactly once.
func TestIterator_MergesAndOrdersByTimestamp(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	objA, closeA := buildObject(t, `{app="a"}`, now, 5)
	defer closeA()
	objB, closeB := buildObject(t, `{app="b"}`, now.Add(500*time.Millisecond), 5)
	defer closeB()

	var sections []*dataobj.Section
	sections = append(sections, collectSections(t, objA)...)
	sections = append(sections, collectSections(t, objB)...)

	iter, err := sortmerge.Iterator(context.Background(), sections, logs.SortTimestampDESC)
	require.NoError(t, err)

	var (
		count int
		prev  time.Time
		first = true
	)
	for res := range iter {
		rec, err := res.Value()
		require.NoError(t, err)
		if !first {
			require.False(t, rec.Timestamp.After(prev),
				"SortTimestampDESC must emit non-increasing timestamps; got %s after %s", rec.Timestamp, prev)
		}
		prev = rec.Timestamp
		first = false
		count++
	}
	require.Equal(t, 10, count, "expected merged record count to equal sum of inputs")
}

// TestIterator_SingleSection_DegenerateIdentity asserts the iterator is a
// straight passthrough when given a single section. Catches wiring breakage
// in the open → dataset → row reader → loser tree path.
func TestIterator_SingleSection_DegenerateIdentity(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	obj, closeFn := buildObject(t, `{app="solo"}`, now, 7)
	defer closeFn()

	sections := collectSections(t, obj)

	iter, err := sortmerge.Iterator(context.Background(), sections, logs.SortStreamASC)
	require.NoError(t, err)

	var count int
	for res := range iter {
		_, err := res.Value()
		require.NoError(t, err)
		count++
	}
	require.Equal(t, 7, count, "expected single-section iterator to emit every input record")
}

// TestIterator_NoSections returns an iterator over zero sections — should
// terminate immediately without error.
func TestIterator_NoSections(t *testing.T) {
	iter, err := sortmerge.Iterator(context.Background(), nil, logs.SortStreamASC)
	require.NoError(t, err)
	for res := range iter {
		_, err := res.Value()
		t.Fatalf("expected no records from zero-section iterator; got record err=%v", err)
	}
}
