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

// readAllRowsFromObject opens every stats section in obj and concatenates
// all rows in obj.Sections() order (stable).
func readAllRowsFromObject(t *testing.T, obj *dataobj.Object) []Stat {
	t.Helper()
	var all []Stat
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err := Open(t.Context(), s)
		require.NoError(t, err)

		rr := NewRowReader(t.Context(), sec, nil)
		for rr.Next() {
			all = append(all, rr.At())
		}
		require.NoError(t, rr.Err())
		require.NoError(t, rr.Close())
	}
	return all
}

// TestBuilder_Empty verifies that an empty builder produces no sections.
func TestBuilder_Empty(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))
	require.Zero(t, b.EstimatedSize(), "empty builder should have zero size")
	require.Empty(t, b.rows, "empty builder should have no rows")
}

// TestBuilder_RoundTrip verifies stats round-trip correctly through on-disk encoding.
func TestBuilder_RoundTrip(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))

	input := []Stat{
		{
			ObjectPath:       "/tenant/abc/obj1",
			SectionIndex:     0,
			SortSchema:       "label:service_name",
			Labels:           map[string]string{"service_name": "foo"},
			MinTimestamp:     1000,
			MaxTimestamp:     2000,
			RowCount:         100,
			UncompressedSize: 8192,
		},
		{
			ObjectPath:       "/tenant/abc/obj2",
			SectionIndex:     1,
			SortSchema:       "label:service_name",
			Labels:           map[string]string{"service_name": "bar"},
			MinTimestamp:     500,
			MaxTimestamp:     1500,
			RowCount:         50,
			UncompressedSize: 4096,
		},
		{
			ObjectPath:       "/tenant/abc/obj3",
			SectionIndex:     2,
			SortSchema:       "label:service_name",
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
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	actual := readAllRowsFromObject(t, obj)
	// Sort order: bar < baz < foo
	expected := []Stat{
		{ObjectPath: "/tenant/abc/obj2", SectionIndex: 1, SortSchema: "label:service_name", Labels: map[string]string{"service_name": "bar"}, MinTimestamp: 500, MaxTimestamp: 1500, RowCount: 50, UncompressedSize: 4096},
		{ObjectPath: "/tenant/abc/obj3", SectionIndex: 2, SortSchema: "label:service_name", Labels: map[string]string{"service_name": "baz"}, MinTimestamp: 3000, MaxTimestamp: 4000, RowCount: 200, UncompressedSize: 16384},
		{ObjectPath: "/tenant/abc/obj1", SortSchema: "label:service_name", Labels: map[string]string{"service_name": "foo"}, MinTimestamp: 1000, MaxTimestamp: 2000, RowCount: 100, UncompressedSize: 8192},
	}
	require.Equal(t, expected, actual)
}

// TestBuilder_SortOrder verifies the sort order: label values in sort-schema order,
// then MinTimestamp, then MaxTimestamp.
func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))

	// Intentionally appended out of order.
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "beta"}, MinTimestamp: 200})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 300})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 100})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "gamma"}, MinTimestamp: 50})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 200})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	actual := readAllRowsFromObject(t, obj)
	// Verify sort order: alpha(100), alpha(200), alpha(300), beta(200), gamma(50).
	expected := []Stat{
		{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 100},
		{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 200},
		{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 300},
		{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "beta"}, MinTimestamp: 200},
		{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "gamma"}, MinTimestamp: 50},
	}
	require.Equal(t, expected, actual)
}

// TestBuilder_AllSameServiceName verifies that rows with identical service_name
// are sorted by MinTimestamp.
func TestBuilder_AllSameServiceName(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))

	// Multiple rows with the same service_name, different timestamps.
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 300, ObjectPath: "c"})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100, ObjectPath: "a"})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 200, ObjectPath: "b"})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	actual := readAllRowsFromObject(t, obj)
	// Sort is by MinTimestamp within the same service_name.
	expected := []Stat{
		{ObjectPath: "a", SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100},
		{ObjectPath: "b", SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 200},
		{ObjectPath: "c", SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 300},
	}
	require.Equal(t, expected, actual)
}

// TestCompare_FullKeyOrder locks the canonical stats sort order
func TestCompare_FullKeyOrder(t *testing.T) {
	const schema = "label:service_name,label:namespace"
	base := func() Stat {
		return Stat{
			SortSchema:   schema,
			ShardBucket:  2,
			Labels:       map[string]string{"service_name": "svc", "namespace": "ns"},
			MinTimestamp: 100,
			MaxTimestamp: 200,
			ObjectPath:   "objA",
			SectionIndex: 0,
		}
	}

	// Each case mutates base() into a row that should sort strictly after it by
	// changing exactly one key component, holding all lower-precedence
	// components equal so the named component alone decides the order. Cases are
	// listed from highest- to lowest-precedence key component.
	tests := []struct {
		name   string
		mutate func(*Stat)
	}{
		{"shard bucket", func(s *Stat) { s.ShardBucket = 7 }},
		{"first sort label", func(s *Stat) { s.Labels = map[string]string{"service_name": "zzz", "namespace": "ns"} }},
		{"second sort label", func(s *Stat) { s.Labels = map[string]string{"service_name": "svc", "namespace": "zz"} }},
		{"min timestamp", func(s *Stat) { s.MinTimestamp = 999 }},
		{"max timestamp", func(s *Stat) { s.MaxTimestamp = 999 }},
		{"object path", func(s *Stat) { s.ObjectPath = "objZ" }},
		{"section index", func(s *Stat) { s.SectionIndex = 9 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := base()
			b := base()
			tt.mutate(&b)

			require.Negative(t, Compare(a, b), "a should sort before b")
			require.Positive(t, Compare(b, a), "b should sort after a")
		})
	}

	// Identical rows compare equal.
	require.Zero(t, Compare(base(), base()))

	// A higher-precedence component overrides a lower-precedence one: a sorts
	// before b on service_name even though b has the smaller SectionIndex.
	a := base()
	a.Labels = map[string]string{"service_name": "aaa", "namespace": "ns"}
	a.SectionIndex = 9
	b := base()
	b.Labels = map[string]string{"service_name": "zzz", "namespace": "ns"}
	b.SectionIndex = 0
	require.Negative(t, Compare(a, b), "service_name must outrank section_index")
	require.Positive(t, Compare(b, a), "service_name must outrank section_index")
}

// TestBuilder_TieBreakOnObjectPathAndSectionIndex verifies that rows sharing
// (labels, MinTimestamp, MaxTimestamp) are ordered by ObjectPath then
// SectionIndex, matching the full-key order of [Compare] used by the
// index-merge executor. Without this tie-break, multi-section output spill
// produces rows whose order disagrees with the reader/verifier's expectation,
// surfacing as "output stats not sorted".
func TestBuilder_TieBreakOnObjectPathAndSectionIndex(t *testing.T) {
	// All rows share the same (service_name, MinTimestamp, MaxTimestamp).
	// They differ only in ObjectPath and SectionIndex, and are appended in an
	// order that the coarse (labels, minT, maxT) sort would leave untouched.
	const schema = "label:service_name"
	b := NewBuilder(nil, defaultEncoder, testLayout(schema))
	labels := map[string]string{"service_name": "svc"}
	b.Append(Stat{SortSchema: schema, Labels: labels, MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "objB", SectionIndex: 0})
	b.Append(Stat{SortSchema: schema, Labels: labels, MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "objA", SectionIndex: 1})
	b.Append(Stat{SortSchema: schema, Labels: labels, MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "objA", SectionIndex: 0})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	actual := readAllRowsFromObject(t, obj)
	// Full-key order: (objA, 0), (objA, 1), (objB, 0).
	expected := []Stat{
		{ObjectPath: "objA", SortSchema: schema, Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100, MaxTimestamp: 200},
		{ObjectPath: "objA", SectionIndex: 1, SortSchema: schema, Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100, MaxTimestamp: 200},
		{ObjectPath: "objB", SortSchema: schema, Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100, MaxTimestamp: 200},
	}
	require.Equal(t, expected, actual)
}

// TestBuilder_MissingServiceName verifies rows with empty/missing label values sort before non-empty ones.
func TestBuilder_MissingServiceName(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))

	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": ""}, ObjectPath: "obj1", MinTimestamp: 100})
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, ObjectPath: "obj2", MinTimestamp: 200})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	actual := readAllRowsFromObject(t, obj)
	// Empty string sorts before "svc".
	expected := []Stat{
		{ObjectPath: "obj1", SortSchema: "label:service_name", Labels: map[string]string{"service_name": ""}, MinTimestamp: 100},
		{ObjectPath: "obj2", SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 200},
	}
	require.Equal(t, expected, actual)
}

// TestBuilder_SectionSplitting verifies the mid-accumulation flush pattern using dataobj.Builder:
// append rows, flush via dataobjBuilder.Append, append more, verify 2 sections.
func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small page size to force multiple pages.
	smallEncoder := ColumnarSectionEncoder(100, 2)
	b := NewBuilder(nil, smallEncoder, testLayout("label:service_name"))
	b.SetTenant("test-tenant")

	// Append first batch.
	for i := range 3 {
		b.Append(Stat{
			ObjectPath:   "x",
			SortSchema:   "label:service_name",
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
			SortSchema:   "label:service_name",
			Labels:       map[string]string{"service_name": "svc"},
			MinTimestamp: int64((i + 3) * 100),
		})
	}

	// Flush second batch into objBuilder.
	require.NoError(t, objBuilder.Append(b))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	// Verify 2 sections were created.
	var statsSections []*dataobj.Section
	for _, s := range obj.Sections() {
		if CheckSection(s) {
			statsSections = append(statsSections, s)
		}
	}
	require.Len(t, statsSections, 2, "expected 2 stats sections after two flushes")

	// Collect all rows from both sections.
	actual := readAllRowsFromObject(t, obj)
	require.Len(t, actual, 6)
}

// TestBuilder_LargeValues verifies large label and path values round-trip correctly.
func TestBuilder_LargeValues(t *testing.T) {
	longPath := "/" + strings.Repeat("a", 10000)
	longLabel := strings.Repeat("b", 5000)
	longName := strings.Repeat("c", 2000)
	longSchema := "label:" + longName
	b := NewBuilder(nil, defaultEncoder, testLayout(longSchema))

	input := Stat{
		ObjectPath:       longPath,
		SortSchema:       longSchema,
		Labels:           map[string]string{longName: longLabel},
		SectionIndex:     99,
		MinTimestamp:     1_000_000,
		MaxTimestamp:     2_000_000,
		RowCount:         99999,
		UncompressedSize: 1_000_000_000,
	}
	b.Append(input)

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	require.Equal(t, []Stat{input}, readAllRowsFromObject(t, obj))
}

// TestBuilder_ResetAndReuse verifies that Reset clears all rows and the builder can be reused.
func TestBuilder_ResetAndReuse(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))
	b.SetTenant("test-tenant")

	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "first"}, MinTimestamp: 100})
	b.Reset()

	// After Reset, builder should be empty and produce no sections when flushed.
	require.Empty(t, b.rows, "builder should be empty after reset")
	require.Zero(t, b.EstimatedSize(), "estimated size should be zero after reset")

	// Add new data after reset and flush.
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "second"}, MinTimestamp: 200})

	obj, closer := buildObject(t, b)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	actual := readAllRowsFromObject(t, obj)
	expected := []Stat{
		{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "second"}, MinTimestamp: 200},
	}
	require.Equal(t, expected, actual)
}

// TestBuilder_EstimatedSize verifies EstimatedSize returns non-zero after appending.
func TestBuilder_EstimatedSize(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("sch"))

	require.Zero(t, b.EstimatedSize(), "empty builder should have zero estimated size")

	b.Append(Stat{
		ObjectPath: "obj",                           // 3 bytes
		SortSchema: "sch",                           // 3 bytes
		Labels:     map[string]string{"sch": "svc"}, // key: 3 bytes, value: 3 bytes
	})

	// 6 * 8 = 48 for numeric fields (SectionIndex, ShardBucket, MinTimestamp,
	// MaxTimestamp, RowCount, UncompressedSize)
	// + 3 (ObjectPath) + 3 (SortSchema) + 3 (key) + 3 (value) = 60
	require.Equal(t, 60, b.EstimatedSize())
}

// TestBuilder_FlushResetsBuilder verifies that a flush resets the builder state.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))
	b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc"}, MinTimestamp: 100})

	obj, closer := buildObject(t, b)
	closer.Close()
	require.Len(t, obj.Sections(), 1)

	// After flush, builder should be empty (Reset was called).
	require.Empty(t, b.rows, "builder should be empty after flush")
	require.Zero(t, b.EstimatedSize(), "builder should have zero estimated size after flush")
}

// TestBuilder_Type verifies that the section type is correct.
func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))
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

func TestBuilder_Flush_SortSchema(t *testing.T) {
	t.Run("returns an error when a row has another sort schema than the layout", func(t *testing.T) {
		b := NewBuilder(nil, defaultEncoder, testLayout("label:service_name"))
		b.SetTenant("tenant")
		b.Append(Stat{SortSchema: "label:service_name", Labels: map[string]string{"service_name": "a"}})
		b.Append(Stat{SortSchema: "label:namespace", Labels: map[string]string{"namespace": "b"}})

		objBuilder := dataobj.NewBuilder(nil)
		require.ErrorContains(t, objBuilder.Append(b), `has sort schema "label:namespace", but the section layout has "label:service_name"`)
	})
}
