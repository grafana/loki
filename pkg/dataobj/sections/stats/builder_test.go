package stats

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// defaultEncoder is a SectionEncoder using default page sizes for testing.
var defaultEncoder = ColumnarSectionEncoder(1024*1024, 10000)

// buildObject flushes the builder into a dataobj.Object using dataobj.Builder.
func buildObject(t *testing.T, b *Builder) (*dataobj.Object, io.Closer) {
	t.Helper()
	b.SetTenant("test-tenant")
	objBuilder := dataobj.NewBuilder(nil)
	err := objBuilder.Append(b)
	require.NoError(t, err)
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	return obj, closer
}

// readAllStatsFromObject opens all stats sections in the object and reads all Stat rows.
func readAllStatsFromObject(t *testing.T, obj *dataobj.Object) []Stat {
	t.Helper()
	var all []Stat
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err := Open(context.Background(), s)
		require.NoError(t, err)

		rr := NewRowReader(sec)
		defer rr.Close()
		require.NoError(t, rr.Open(context.Background()))

		buf := make([]Stat, 100)
		for {
			n, err := rr.Read(context.Background(), buf)
			all = append(all, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
	}
	return all
}

// TestBuilder_Empty verifies that an empty builder produces no sections.
func TestBuilder_Empty(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)
	require.Zero(t, b.EstimatedSize(), "empty builder should have zero size")
	require.Empty(t, b.rows, "empty builder should have no rows")
}

// TestBuilder_RoundTrip verifies stats round-trip correctly through on-disk encoding.
func TestBuilder_RoundTrip(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)

	input := []Stat{
		{
			ObjectPath:       "/tenant/abc/obj1",
			SectionIndex:     0,
			SortSchema:       "service_name",
			Labels:           map[string]string{"service_name": "foo"},
			MinTimestamp:     1000,
			MaxTimestamp:     2000,
			RowCount:         100,
			UncompressedSize: 8192,
		},
		{
			ObjectPath:       "/tenant/abc/obj2",
			SectionIndex:     1,
			SortSchema:       "service_name",
			Labels:           map[string]string{"service_name": "bar"},
			MinTimestamp:     500,
			MaxTimestamp:     1500,
			RowCount:         50,
			UncompressedSize: 4096,
		},
		{
			ObjectPath:       "/tenant/abc/obj3",
			SectionIndex:     2,
			SortSchema:       "service_name",
			Labels:           map[string]string{"service_name": "baz"},
			MinTimestamp:     3000,
			MaxTimestamp:     4000,
			RowCount:         200,
			UncompressedSize: 16384,
		},
	}

	for _, s := range input {
		b.Append(s)
	}

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 3)

	// Sort order: bar < baz < foo
	require.Equal(t, "bar", got[0].Labels["service_name"])
	require.Equal(t, "baz", got[1].Labels["service_name"])
	require.Equal(t, "foo", got[2].Labels["service_name"])

	// Verify all fields for the "foo" stat (last after sort).
	fooStat := got[2]
	require.Equal(t, "/tenant/abc/obj1", fooStat.ObjectPath)
	require.Equal(t, int64(0), fooStat.SectionIndex)
	require.Equal(t, "service_name", fooStat.SortSchema)
	require.Equal(t, map[string]string{"service_name": "foo"}, fooStat.Labels)
	require.Equal(t, int64(1000), fooStat.MinTimestamp)
	require.Equal(t, int64(2000), fooStat.MaxTimestamp)
	require.Equal(t, int64(100), fooStat.RowCount)
	require.Equal(t, int64(8192), fooStat.UncompressedSize)
}

// TestBuilder_SortOrder verifies the sort order: label values in sort-schema order,
// then MinTimestamp, then MaxTimestamp.
func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)

	// Intentionally appended out of order.
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "beta"}, MinTimestamp: 200})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 300})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 100})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "gamma"}, MinTimestamp: 50})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 200})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 5)

	// Verify sort order: alpha(100), alpha(200), alpha(300), beta(200), gamma(50).
	require.Equal(t, "alpha", got[0].Labels["service_name"])
	require.Equal(t, int64(100), got[0].MinTimestamp)
	require.Equal(t, "alpha", got[1].Labels["service_name"])
	require.Equal(t, int64(200), got[1].MinTimestamp)
	require.Equal(t, "alpha", got[2].Labels["service_name"])
	require.Equal(t, int64(300), got[2].MinTimestamp)
	require.Equal(t, "beta", got[3].Labels["service_name"])
	require.Equal(t, int64(200), got[3].MinTimestamp)
	require.Equal(t, "gamma", got[4].Labels["service_name"])
	require.Equal(t, int64(50), got[4].MinTimestamp)
}

// TestBuilder_AllSameServiceName verifies that rows with identical service_name
// are sorted by MinTimestamp.
func TestBuilder_AllSameServiceName(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)

	// Multiple rows with the same service_name, different timestamps.
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 300, ObjectPath: "c"})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100, ObjectPath: "a"})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 200, ObjectPath: "b"})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 3)

	// Sort is by MinTimestamp within the same service_name.
	require.Equal(t, int64(100), got[0].MinTimestamp)
	require.Equal(t, int64(200), got[1].MinTimestamp)
	require.Equal(t, int64(300), got[2].MinTimestamp)
}

// TestBuilder_MissingServiceName verifies rows with empty/missing label values sort before non-empty ones.
func TestBuilder_MissingServiceName(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)

	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": ""}, ObjectPath: "obj1", MinTimestamp: 100})
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "svc"}, ObjectPath: "obj2", MinTimestamp: 200})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 2)

	// Empty string sorts before "svc".
	require.Equal(t, "", got[0].Labels["service_name"])
	require.Equal(t, "svc", got[1].Labels["service_name"])
}

// TestBuilder_SectionSplitting verifies the mid-accumulation flush pattern using dataobj.Builder:
// append rows, flush via dataobjBuilder.Append, append more, verify 2 sections.
func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small page size to force multiple pages.
	smallEncoder := ColumnarSectionEncoder(100, 2)
	b := NewBuilder(nil, smallEncoder)
	b.SetTenant("test-tenant")

	// Append first batch.
	for i := range 3 {
		b.Append(Stat{
			ObjectPath:   "x",
			SortSchema:   "service_name",
			Labels:       map[string]string{"service_name": "svc"},
			MinTimestamp: int64(i * 100),
		})
	}

	objBuilder := dataobj.NewBuilder(nil)

	// Flush first batch into objBuilder.
	require.NoError(t, objBuilder.Append(b))

	// Append second batch.
	for i := range 3 {
		b.Append(Stat{
			ObjectPath:   "y",
			SortSchema:   "service_name",
			Labels:       map[string]string{"service_name": "svc"},
			MinTimestamp: int64((i + 3) * 100),
		})
	}

	// Flush second batch into objBuilder.
	require.NoError(t, objBuilder.Append(b))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	// Verify 2 sections were created.
	var statsSections []*dataobj.Section
	for _, s := range obj.Sections() {
		if CheckSection(s) {
			statsSections = append(statsSections, s)
		}
	}
	require.Len(t, statsSections, 2, "expected 2 stats sections after two flushes")

	// Collect all rows from both sections.
	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 6)
}

// TestBuilder_LargeValues verifies large label and path values round-trip correctly.
func TestBuilder_LargeValues(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)

	longPath := "/" + strings.Repeat("a", 10000)
	longLabel := strings.Repeat("b", 5000)
	longSchema := strings.Repeat("c", 2000)

	b.Append(Stat{
		ObjectPath:       longPath,
		SortSchema:       longSchema,
		Labels:           map[string]string{longSchema: longLabel},
		SectionIndex:     99,
		MinTimestamp:     1_000_000,
		MaxTimestamp:     2_000_000,
		RowCount:         99999,
		UncompressedSize: 1_000_000_000,
	})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 1)

	stat := got[0]
	require.Equal(t, longPath, stat.ObjectPath)
	require.Equal(t, longSchema, stat.SortSchema)
	require.Equal(t, longLabel, stat.Labels[longSchema])
	require.Equal(t, int64(99), stat.SectionIndex)
	require.Equal(t, int64(1_000_000), stat.MinTimestamp)
	require.Equal(t, int64(2_000_000), stat.MaxTimestamp)
	require.Equal(t, int64(99999), stat.RowCount)
	require.Equal(t, int64(1_000_000_000), stat.UncompressedSize)
}

// TestBuilder_ResetAndReuse verifies that Reset clears all rows and the builder can be reused.
func TestBuilder_ResetAndReuse(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)
	b.SetTenant("test-tenant")

	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "first"}, MinTimestamp: 100})
	b.Reset()

	// After Reset, builder should be empty and produce no sections when flushed.
	require.Empty(t, b.rows, "builder should be empty after reset")
	require.Zero(t, b.EstimatedSize(), "estimated size should be zero after reset")

	// Add new data after reset and flush.
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "second"}, MinTimestamp: 200})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	got := readAllStatsFromObject(t, obj)
	require.Len(t, got, 1)
	require.Equal(t, "second", got[0].Labels["service_name"])
}

// TestBuilder_EstimatedSize verifies EstimatedSize returns non-zero after appending.
func TestBuilder_EstimatedSize(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)

	require.Zero(t, b.EstimatedSize(), "empty builder should have zero estimated size")

	b.Append(Stat{
		ObjectPath: "obj",                           // 3 bytes
		SortSchema: "sch",                           // 3 bytes
		Labels:     map[string]string{"sch": "svc"}, // key: 3 bytes, value: 3 bytes
	})

	// 5 * 8 = 40 for int64s (SectionIndex, MinTimestamp, MaxTimestamp, RowCount, UncompressedSize)
	// + 3 (ObjectPath) + 3 (SortSchema) + 3 (key) + 3 (value) = 52
	require.Equal(t, 52, b.EstimatedSize())
}

// TestBuilder_FlushResetsBuilder verifies that a flush resets the builder state.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)
	b.Append(Stat{SortSchema: "service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100})

	obj, closer := buildObject(t, b)
	closer.Close()
	require.Len(t, obj.Sections(), 1)

	// After flush, builder should be empty (Reset was called).
	require.Empty(t, b.rows, "builder should be empty after flush")
	require.Zero(t, b.EstimatedSize(), "builder should have zero estimated size after flush")
}

// TestBuilder_Type verifies that the section type is correct.
func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder)
	require.Equal(t, sectionType, b.Type())
}

// TestOpen_WrongSectionType verifies that Open rejects sections with the wrong type.
func TestOpen_WrongSectionType(t *testing.T) {
	wrongType := &dataobj.Section{
		Type: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "postings",
			Version:   1,
		},
	}
	_, err := Open(context.Background(), wrongType)
	require.Error(t, err)
	require.Contains(t, err.Error(), "section type mismatch")
}

// TestOpen_WrongVersion verifies that Open rejects sections with the wrong version.
func TestOpen_WrongVersion(t *testing.T) {
	wrongVersion := &dataobj.Section{
		Type: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "stats",
			Version:   99,
		},
	}
	_, err := Open(context.Background(), wrongVersion)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported section version")
}
