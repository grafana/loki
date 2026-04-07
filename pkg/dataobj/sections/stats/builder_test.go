package stats

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestBuilder_Empty(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)
	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Empty(t, sections, "empty builder should produce no sections")
}

func TestBuilder_RoundTrip(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	input := []Stat{
		{
			ObjectPath:       "/tenant/abc/obj1",
			SectionIndex:     0,
			RunID:            42,
			SortSchema:       "service_name",
			ServiceName:      "foo",
			MinTimestamp:     1000,
			MaxTimestamp:     2000,
			RowCount:         100,
			UncompressedSize: 8192,
		},
		{
			ObjectPath:       "/tenant/abc/obj2",
			SectionIndex:     1,
			RunID:            43,
			SortSchema:       "service_name",
			ServiceName:      "bar",
			MinTimestamp:     500,
			MaxTimestamp:     1500,
			RowCount:         50,
			UncompressedSize: 4096,
		},
		{
			ObjectPath:       "/tenant/abc/obj3",
			SectionIndex:     2,
			RunID:            44,
			SortSchema:       "service_name",
			ServiceName:      "baz",
			MinTimestamp:     3000,
			MaxTimestamp:     4000,
			RowCount:         200,
			UncompressedSize: 16384,
		},
	}

	for _, s := range input {
		b.Append(s)
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	got := make([]Stat, 10)
	n, err := rr.Read(context.Background(), got)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 3, n)
	got = got[:n]

	// All rows should round-trip; sort order is by ServiceName then MinTimestamp.
	// bar < baz < foo
	require.Equal(t, "bar", got[0].ServiceName)
	require.Equal(t, "baz", got[1].ServiceName)
	require.Equal(t, "foo", got[2].ServiceName)

	// Verify all fields for the "foo" stat (last after sort).
	fooStat := got[2]
	require.Equal(t, "/tenant/abc/obj1", fooStat.ObjectPath)
	require.Equal(t, int64(0), fooStat.SectionIndex)
	require.Equal(t, int64(42), fooStat.RunID)
	require.Equal(t, "service_name", fooStat.SortSchema)
	require.Equal(t, "foo", fooStat.ServiceName)
	require.Equal(t, int64(1000), fooStat.MinTimestamp)
	require.Equal(t, int64(2000), fooStat.MaxTimestamp)
	require.Equal(t, int64(100), fooStat.RowCount)
	require.Equal(t, int64(8192), fooStat.UncompressedSize)
}

func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	// Intentionally appended out of order.
	b.Append(Stat{ServiceName: "beta", MinTimestamp: 200})
	b.Append(Stat{ServiceName: "alpha", MinTimestamp: 300})
	b.Append(Stat{ServiceName: "alpha", MinTimestamp: 100})
	b.Append(Stat{ServiceName: "gamma", MinTimestamp: 50})
	b.Append(Stat{ServiceName: "alpha", MinTimestamp: 200})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	got := make([]Stat, 10)
	n, err := rr.Read(context.Background(), got)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 5, n)
	got = got[:n]

	// Verify sort order: alpha(100), alpha(200), alpha(300), beta(200), gamma(50).
	require.Equal(t, "alpha", got[0].ServiceName)
	require.Equal(t, int64(100), got[0].MinTimestamp)
	require.Equal(t, "alpha", got[1].ServiceName)
	require.Equal(t, int64(200), got[1].MinTimestamp)
	require.Equal(t, "alpha", got[2].ServiceName)
	require.Equal(t, int64(300), got[2].MinTimestamp)
	require.Equal(t, "beta", got[3].ServiceName)
	require.Equal(t, int64(200), got[3].MinTimestamp)
	require.Equal(t, "gamma", got[4].ServiceName)
	require.Equal(t, int64(50), got[4].MinTimestamp)
}

func TestBuilder_AllSameServiceName(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	// Multiple rows with the same ServiceName, different timestamps.
	b.Append(Stat{ServiceName: "svc", MinTimestamp: 300, ObjectPath: "c"})
	b.Append(Stat{ServiceName: "svc", MinTimestamp: 100, ObjectPath: "a"})
	b.Append(Stat{ServiceName: "svc", MinTimestamp: 200, ObjectPath: "b"})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	got := make([]Stat, 10)
	n, err := rr.Read(context.Background(), got)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 3, n)
	got = got[:n]

	// Sort is by MinTimestamp within the same ServiceName.
	require.Equal(t, int64(100), got[0].MinTimestamp)
	require.Equal(t, int64(200), got[1].MinTimestamp)
	require.Equal(t, int64(300), got[2].MinTimestamp)
}

func TestBuilder_MissingServiceName(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	b.Append(Stat{ServiceName: "", ObjectPath: "obj1", MinTimestamp: 100})
	b.Append(Stat{ServiceName: "svc", ObjectPath: "obj2", MinTimestamp: 200})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	got := make([]Stat, 10)
	n, err := rr.Read(context.Background(), got)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 2, n)
	got = got[:n]

	// Empty string sorts before "svc".
	require.Equal(t, "", got[0].ServiceName)
	require.Equal(t, "svc", got[1].ServiceName)
}

func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small targetSectionSize to force splitting.
	// Each row with ObjectPath="obj" (3 bytes) + SortSchema="" (0) + ServiceName (varies):
	// rowSize = 40 + len(ObjectPath) + len(SortSchema) + len(ServiceName)
	// With ObjectPath="x" (1 byte) and ServiceName="svc" (3 bytes), rowSize = 44.
	// Set targetSectionSize to 100 to get at most 2 rows per section.
	b := NewBuilder(100, ColumnarEncoder)

	// Add 6 rows, which should split into at least 3 sections.
	for i := range 6 {
		b.Append(Stat{
			ObjectPath:   "x",
			ServiceName:  "svc",
			MinTimestamp: int64(i * 100),
		})
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Greater(t, len(sections), 1, "expected multiple sections due to splitting")

	// Collect all rows across sections and verify total count.
	var allStats []Stat
	for _, sec := range sections {
		rr, err := NewRowReader(&sec)
		require.NoError(t, err)
		defer rr.Close()

		buf := make([]Stat, 10)
		for {
			n, err := rr.Read(context.Background(), buf)
			allStats = append(allStats, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
	}
	require.Len(t, allStats, 6)

	// Rows should be in sorted order across sections.
	for i := 1; i < len(allStats); i++ {
		require.LessOrEqual(t, allStats[i-1].MinTimestamp, allStats[i].MinTimestamp)
	}
}

func TestBuilder_LargeValues(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	longPath := "/" + strings.Repeat("a", 10000)
	longLabel := strings.Repeat("b", 5000)
	longSchema := strings.Repeat("c", 2000)

	b.Append(Stat{
		ObjectPath:       longPath,
		SortSchema:       longSchema,
		ServiceName:      longLabel,
		SectionIndex:     99,
		RunID:            123456,
		MinTimestamp:     1_000_000,
		MaxTimestamp:     2_000_000,
		RowCount:         99999,
		UncompressedSize: 1_000_000_000,
	})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	got := make([]Stat, 2)
	n, err := rr.Read(context.Background(), got)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 1, n)

	stat := got[0]
	require.Equal(t, longPath, stat.ObjectPath)
	require.Equal(t, longSchema, stat.SortSchema)
	require.Equal(t, longLabel, stat.ServiceName)
	require.Equal(t, int64(99), stat.SectionIndex)
	require.Equal(t, int64(123456), stat.RunID)
	require.Equal(t, int64(1_000_000), stat.MinTimestamp)
	require.Equal(t, int64(2_000_000), stat.MaxTimestamp)
	require.Equal(t, int64(99999), stat.RowCount)
	require.Equal(t, int64(1_000_000_000), stat.UncompressedSize)
}

func TestBuilder_ResetAndReuse(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	b.Append(Stat{ServiceName: "first", MinTimestamp: 100})
	b.Reset()

	// After Reset, Flush should produce no sections.
	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Empty(t, sections)

	// Add new data after reset.
	b.Append(Stat{ServiceName: "second", MinTimestamp: 200})
	sections, err = b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	got := make([]Stat, 5)
	n, err := rr.Read(context.Background(), got)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 1, n)
	require.Equal(t, "second", got[0].ServiceName)
}

func TestBuilder_EstimatedSize(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	require.Equal(t, 0, b.EstimatedSize(), "empty builder should have zero estimated size")

	b.Append(Stat{
		ObjectPath:  "obj", // 3 bytes
		SortSchema:  "sch", // 3 bytes
		ServiceName: "svc", // 3 bytes
	})
	// 6 * 8 = 48 for int64s (SectionIndex, RunID, MinTimestamp, MaxTimestamp, RowCount, UncompressedSize), plus 3+3+3 = 9 for strings = 57
	require.Equal(t, 57, b.EstimatedSize())
}

func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)
	b.Append(Stat{ServiceName: "svc", MinTimestamp: 100})

	_, err := b.Flush(context.Background())
	require.NoError(t, err)

	// After flush, builder should be empty.
	require.Equal(t, 0, b.EstimatedSize())
	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Empty(t, sections)
}

func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)
	require.Equal(t, sectionType, b.Type())
}

func TestCheckSection(t *testing.T) {
	t.Run("returns true for stats section type", func(t *testing.T) {
		sec := &dataobj.Section{Type: sectionType}
		require.True(t, CheckSection(sec))
	})

	t.Run("returns false for non-stats section type", func(t *testing.T) {
		sec := &dataobj.Section{Type: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "streams",
			Version:   1,
		}}
		require.False(t, CheckSection(sec))
	})
}
