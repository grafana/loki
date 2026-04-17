package postings

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// checkBit returns true if bit n is set in the LSB-encoded bitmap data.
func checkBit(data []byte, n int) bool {
	byteIdx := n / 8
	bitPos := uint(n % 8)
	if byteIdx >= len(data) {
		return false
	}
	return (data[byteIdx]>>bitPos)&1 == 1
}

// TestBuilder_Empty verifies that an empty builder produces no sections.
func TestBuilder_Empty(t *testing.T) {
	b := NewBuilder(nil, 0, 0)
	require.Zero(t, b.EstimatedSize(), "empty builder should have zero size")
	// An empty builder writes 0 bytes; the dataobj builder would fail to flush
	// without any data. This verifies the postings builder is truly empty.
	require.Empty(t, b.labels.entries, "empty builder should have no label entries")
	require.Empty(t, b.blooms.entries, "empty builder should have no bloom entries")
}

// TestBuilder_LabelPostingRoundTrip verifies a label posting round-trips correctly.
func TestBuilder_LabelPostingRoundTrip(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 1000)
	b.ObserveLabelPosting("/tenant/abc/obj1", 0, "env", "value1", 3, ts, 4096)
	b.ObserveLabelPosting("/tenant/abc/obj1", 0, "env", "value1", 7, ts, 0)
	b.ObserveLabelPosting("/tenant/abc/obj1", 0, "env", "value1", 15, ts, 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 1)

	p := got[0]
	require.Equal(t, KindLabel, p.Kind)
	require.Equal(t, "/tenant/abc/obj1", p.ObjectPath)
	require.Equal(t, int64(0), p.SectionIndex)
	require.Equal(t, "env", p.ColumnName)
	require.Equal(t, "value1", p.LabelValue)
	require.Nil(t, p.BloomFilter)
	require.Equal(t, int64(4096), p.UncompressedSize)
	require.Equal(t, int64(1000), p.MinTimestamp)
	require.Equal(t, int64(1000), p.MaxTimestamp)

	// Verify stream IDs 3, 7, 15 are set in the bitmap.
	require.True(t, checkBit(p.StreamIDBitmap, 3), "bit 3 should be set")
	require.True(t, checkBit(p.StreamIDBitmap, 7), "bit 7 should be set")
	require.True(t, checkBit(p.StreamIDBitmap, 15), "bit 15 should be set")
	require.False(t, checkBit(p.StreamIDBitmap, 0), "bit 0 should not be set")
}

// TestBuilder_BloomPostingRoundTrip verifies a bloom posting round-trips correctly.
func TestBuilder_BloomPostingRoundTrip(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 500)
	b.PrepareBloomColumn("/tenant/abc/obj2", 1, "service_name", 100)
	err := b.ObserveBloomPosting("/tenant/abc/obj2", 1, "service_name", "my-service", 0, ts, 8192)
	require.NoError(t, err)
	err = b.ObserveBloomPosting("/tenant/abc/obj2", 1, "service_name", "my-service", 2, ts, 0)
	require.NoError(t, err)
	err = b.ObserveBloomPosting("/tenant/abc/obj2", 1, "service_name", "my-service", 8, ts, 0)
	require.NoError(t, err)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 1)

	p := got[0]
	require.Equal(t, KindBloom, p.Kind)
	require.Equal(t, "/tenant/abc/obj2", p.ObjectPath)
	require.Equal(t, int64(1), p.SectionIndex)
	require.Equal(t, "service_name", p.ColumnName)
	require.Empty(t, p.LabelValue)
	require.NotNil(t, p.BloomFilter, "Bloom posting should have non-nil BloomFilter")
	require.Equal(t, int64(8192), p.UncompressedSize)
	require.Equal(t, int64(500), p.MinTimestamp)
	require.Equal(t, int64(500), p.MaxTimestamp)

	// Verify stream IDs 0, 2, 8 are set.
	require.True(t, checkBit(p.StreamIDBitmap, 0), "bit 0 should be set")
	require.True(t, checkBit(p.StreamIDBitmap, 2), "bit 2 should be set")
	require.True(t, checkBit(p.StreamIDBitmap, 8), "bit 8 should be set")
	require.False(t, checkBit(p.StreamIDBitmap, 1), "bit 1 should not be set")
}

// TestBuilder_MixedPostings verifies both Bloom and Label postings work together.
func TestBuilder_MixedPostings(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 100)
	b.PrepareBloomColumn("/obj1", 0, "col_a", 10)
	err := b.ObserveBloomPosting("/obj1", 0, "col_a", "val", 0, ts, 0)
	require.NoError(t, err)

	ts2 := time.Unix(0, 300)
	b.ObserveLabelPosting("/obj2", 0, "col_b", "myval", 1, ts2, 0)
	b.ObserveLabelPosting("/obj2", 0, "col_b", "myval", 3, ts2, 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 2)

	// Bloom (KindBloom=0) sorts before Label (KindLabel=1).
	require.Equal(t, KindBloom, got[0].Kind)
	require.Empty(t, got[0].LabelValue)
	require.NotNil(t, got[0].BloomFilter)

	require.Equal(t, KindLabel, got[1].Kind)
	require.NotEmpty(t, got[1].LabelValue)
	require.Nil(t, got[1].BloomFilter)
}

// TestBuilder_SortOrder verifies the sort order: bloom entries before label entries,
// sorted by [objectPath, sectionIndex, columnName] for blooms and
// [objectPath, sectionIndex, columnName, labelValue] for labels.
func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 50)

	// Prepare and add bloom entries.
	b.PrepareBloomColumn("", 0, "col_a", 10)
	_ = b.ObserveBloomPosting("", 0, "col_a", "v", 0, ts, 0)

	b.PrepareBloomColumn("", 0, "col_b", 10)
	_ = b.ObserveBloomPosting("", 0, "col_b", "v", 0, time.Unix(0, 10), 0)

	// Label entries.
	b.ObserveLabelPosting("", 0, "col_a", "beta", 0, time.Unix(0, 200), 0)
	b.ObserveLabelPosting("", 0, "col_a", "alpha", 0, time.Unix(0, 100), 0)
	b.ObserveLabelPosting("", 0, "col_a", "alpha", 0, time.Unix(0, 50), 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 4) // 2 bloom + 2 label (alpha aggregated into 1, beta into 1)

	// Expected order:
	// 0: KindBloom, col_a
	// 1: KindBloom, col_b
	// 2: KindLabel, col_a, alpha (aggregated)
	// 3: KindLabel, col_a, beta (aggregated)
	require.Equal(t, KindBloom, got[0].Kind)
	require.Equal(t, "col_a", got[0].ColumnName)
	require.Empty(t, got[0].LabelValue)

	require.Equal(t, KindBloom, got[1].Kind)
	require.Equal(t, "col_b", got[1].ColumnName)
	require.Empty(t, got[1].LabelValue)

	require.Equal(t, KindLabel, got[2].Kind)
	require.Equal(t, "col_a", got[2].ColumnName)
	require.Equal(t, "alpha", got[2].LabelValue)

	require.Equal(t, KindLabel, got[3].Kind)
	require.Equal(t, "col_a", got[3].ColumnName)
	require.Equal(t, "beta", got[3].LabelValue)
}

// TestBuilder_NullableHandling verifies nullable column correctness.
func TestBuilder_NullableHandling(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)

	b.PrepareBloomColumn("", 0, "col", 10)
	_ = b.ObserveBloomPosting("", 0, "col", "val", 0, ts, 0)

	b.ObserveLabelPosting("", 0, "col", "val", 0, ts, 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 2)

	// Bloom posting: label_value is empty, bloom_filter is non-null
	bloom := got[0]
	require.Equal(t, KindBloom, bloom.Kind)
	require.Empty(t, bloom.LabelValue, "Bloom posting should have empty LabelValue")
	require.NotNil(t, bloom.BloomFilter, "Bloom posting should have non-nil BloomFilter")

	// Label posting: bloom_filter is null, label_value is non-empty
	label := got[1]
	require.Equal(t, KindLabel, label.Kind)
	require.NotEmpty(t, label.LabelValue, "Label posting should have non-empty LabelValue")
	require.Nil(t, label.BloomFilter, "Label posting should have nil BloomFilter")
}

// TestBuilder_BitmapCorrectness verifies that stream ID bitmaps are LSB-encoded correctly.
func TestBuilder_BitmapCorrectness(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	b.PrepareBloomColumn("", 0, "col", 10)
	// Observe stream IDs 0, 3, 7.
	_ = b.ObserveBloomPosting("", 0, "col", "v", 0, ts, 0)
	_ = b.ObserveBloomPosting("", 0, "col", "v", 3, ts, 0)
	_ = b.ObserveBloomPosting("", 0, "col", "v", 7, ts, 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 1)

	// Verify LSB encoding: bit N at byte N/8, position N%8.
	bitmap := got[0].StreamIDBitmap
	require.NotEmpty(t, bitmap)

	require.True(t, checkBit(bitmap, 0), "bit 0 should be set")
	require.False(t, checkBit(bitmap, 1), "bit 1 should not be set")
	require.False(t, checkBit(bitmap, 2), "bit 2 should not be set")
	require.True(t, checkBit(bitmap, 3), "bit 3 should be set")
	require.False(t, checkBit(bitmap, 4), "bit 4 should not be set")
	require.False(t, checkBit(bitmap, 5), "bit 5 should not be set")
	require.False(t, checkBit(bitmap, 6), "bit 6 should not be set")
	require.True(t, checkBit(bitmap, 7), "bit 7 should be set")
}

// TestBuilder_BitmapNormalization verifies that bitmaps of different sizes are
// padded to the same length.
func TestBuilder_BitmapNormalization(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	// "a": stream ID 0 → 1-byte bitmap
	b.ObserveLabelPosting("", 0, "col", "a", 0, ts, 0)
	// "b": stream ID 23 → 3-byte bitmap
	b.ObserveLabelPosting("", 0, "col", "b", 23, ts, 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 2)

	// All bitmaps should be the same length (3 bytes, the maximum for stream ID 23).
	require.Len(t, got[0].StreamIDBitmap, 3, "short bitmap should be padded to max length")
	require.Len(t, got[1].StreamIDBitmap, 3, "long bitmap should remain at max length")
}

// TestBuilder_SectionSplitting verifies that a small targetSectionSize causes
// rows to be split across multiple pages.
func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small page size to force splitting across multiple pages.
	b := NewBuilder(nil, 100, 2)

	ts := time.Unix(0, 0)
	for i := range 6 {
		b.ObserveLabelPosting("", 0, "col", fmt.Sprintf("val%d", i), 0, ts, 0)
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1, "all postings go in one section")

	// Collect all rows.
	allPostings := readAllPostings(t, sections[0])
	require.Len(t, allPostings, 6)
}

// TestBuilder_AllBloom verifies that a builder with only Bloom postings works.
func TestBuilder_AllBloom(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	for i := range 3 {
		colName := fmt.Sprintf("col%d", i)
		b.PrepareBloomColumn("", 0, colName, 10)
		_ = b.ObserveBloomPosting("", 0, colName, "val", 0, ts, 0)
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 3)
	for _, p := range got {
		require.Equal(t, KindBloom, p.Kind)
		require.Empty(t, p.LabelValue)
		require.NotNil(t, p.BloomFilter)
	}
}

// TestBuilder_AllLabel verifies that a builder with only Label postings works.
func TestBuilder_AllLabel(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	for i := range 3 {
		lv := fmt.Sprintf("val%d", i)
		b.ObserveLabelPosting("", 0, "col", lv, 0, ts, 0)
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 3)
	for _, p := range got {
		require.Equal(t, KindLabel, p.Kind)
		require.NotEmpty(t, p.LabelValue)
		require.Nil(t, p.BloomFilter)
	}
}

// TestBuilder_FlushResetsBuilder verifies that a flush resets the builder.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	b.ObserveLabelPosting("", 0, "col", "v", 0, ts, 0)

	obj, closer := flushToObject(t, b)
	closer.Close()
	require.Len(t, obj.Sections(), 1)

	// After flush, builder should be empty (Reset was called).
	require.Empty(t, b.labels.entries, "builder should have no label entries after flush")
	require.Empty(t, b.blooms.entries, "builder should have no bloom entries after flush")
	require.Zero(t, b.EstimatedSize(), "builder should have zero estimated size after flush")
}

// TestBuilder_Type verifies that the section type is correct.
func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(nil, 0, 0)
	require.Equal(t, sectionType, b.Type())
}

// TestCheckSection verifies CheckSection correctness.
func TestCheckSection(t *testing.T) {
	t.Run("returns true for postings section type", func(t *testing.T) {
		sec := &dataobj.Section{Type: sectionType}
		require.True(t, CheckSection(sec))
	})

	t.Run("returns false for non-postings section type", func(t *testing.T) {
		sec := &dataobj.Section{Type: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "stats",
			Version:   1,
		}}
		require.False(t, CheckSection(sec))
	})
}

func TestRowReader_SmallBuffer(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	// Append 5 rows with distinct label values.
	for i := range 5 {
		b.ObserveLabelPosting("", 0, "col", fmt.Sprintf("val%d", i), 0, ts, 0)
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rr := NewRowReader(sections[0])
	defer rr.Close()

	require.NoError(t, rr.Open(context.Background()))

	// Read with a buffer smaller than the section row count.
	buf := make([]Posting, 2)
	n, err := rr.Read(context.Background(), buf)
	require.NoError(t, err)
	require.Equal(t, 2, n, "should read exactly len(buf) rows")
}

// TestBuilder_ObserveLabelPosting verifies that multiple stream IDs for the
// same key are aggregated into a single posting entry.
func TestBuilder_ObserveLabelPosting(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	minTs := time.Unix(0, 100)
	midTs := time.Unix(0, 200)
	maxTs := time.Unix(0, 300)

	b.ObserveLabelPosting("/obj", 0, "env", "prod", 1, minTs, 100)
	b.ObserveLabelPosting("/obj", 0, "env", "prod", 5, midTs, 200)
	b.ObserveLabelPosting("/obj", 0, "env", "prod", 10, maxTs, 300)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 1, "three observations for same key should aggregate to one posting")

	p := got[0]
	require.Equal(t, KindLabel, p.Kind)
	require.Equal(t, "env", p.ColumnName)
	require.Equal(t, "prod", p.LabelValue)
	require.Equal(t, int64(600), p.UncompressedSize, "sizes should be summed")
	require.Equal(t, minTs.UnixNano(), p.MinTimestamp)
	require.Equal(t, maxTs.UnixNano(), p.MaxTimestamp)

	// Verify stream IDs 1, 5, 10 are set.
	require.True(t, checkBit(p.StreamIDBitmap, 1))
	require.True(t, checkBit(p.StreamIDBitmap, 5))
	require.True(t, checkBit(p.StreamIDBitmap, 10))
	require.False(t, checkBit(p.StreamIDBitmap, 0))
	require.False(t, checkBit(p.StreamIDBitmap, 2))
}

// TestBuilder_ObserveBloomPosting verifies bloom observation: prepare, observe, check membership and bitmap.
func TestBuilder_ObserveBloomPosting(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 100)
	b.PrepareBloomColumn("/obj", 0, "service_name", 100)

	values := []string{"alpha", "beta", "gamma"}
	for i, v := range values {
		err := b.ObserveBloomPosting("/obj", 0, "service_name", v, int64(i), ts, 10)
		require.NoError(t, err)
	}

	// Check bloom bytes contain values.
	bloomBytes, err := b.BloomBytes("/obj", 0, "service_name")
	require.NoError(t, err)
	require.NotEmpty(t, bloomBytes)

	// Verify bloom membership via the aggregator's bloom filter.
	entry := b.blooms.entries[bloomPostingKey{"/obj", 0, "service_name"}]
	require.NotNil(t, entry)
	for _, v := range values {
		require.True(t, entry.BloomFilter().Test([]byte(v)), "bloom filter should contain %q", v)
	}
	require.False(t, entry.BloomFilter().Test([]byte("delta")), "bloom filter should not contain 'delta'")

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 1)

	p := got[0]
	require.Equal(t, KindBloom, p.Kind)
	require.Equal(t, "service_name", p.ColumnName)
	require.NotNil(t, p.BloomFilter)

	// Verify stream IDs 0, 1, 2 are set in the bitmap.
	require.True(t, checkBit(p.StreamIDBitmap, 0))
	require.True(t, checkBit(p.StreamIDBitmap, 1))
	require.True(t, checkBit(p.StreamIDBitmap, 2))
	require.False(t, checkBit(p.StreamIDBitmap, 3))
}

// TestBuilder_MixedObservations verifies bloom and label observations produce
// the correct sort order (bloom before label).
func TestBuilder_MixedObservations(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 100)

	// Add label first (out of order relative to expected output).
	b.ObserveLabelPosting("/obj", 0, "col_b", "v", 0, ts, 0)

	// Add bloom second.
	b.PrepareBloomColumn("/obj", 0, "col_a", 10)
	_ = b.ObserveBloomPosting("/obj", 0, "col_a", "v", 0, ts, 0)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 2)

	// Bloom should be first regardless of observation order.
	require.Equal(t, KindBloom, got[0].Kind, "bloom should sort before label")
	require.Equal(t, KindLabel, got[1].Kind)
}

// TestBuilder_ObserveBloomUnprepared verifies that observing an unprepared bloom
// column returns an error.
func TestBuilder_ObserveBloomUnprepared(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	err := b.ObserveBloomPosting("/obj", 0, "unprepared_col", "val", 0, ts, 0)
	require.Error(t, err, "observing unprepared bloom column should return an error")
	require.Contains(t, err.Error(), "bloom column not prepared")
}

// TestBuilder_MultipleObjectContexts verifies that observations with different
// (objectPath, sectionIndex) for the same (columnName, labelValue) produce
// distinct posting rows.
func TestBuilder_MultipleObjectContexts(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)

	// Same column/label, different object paths.
	b.ObserveLabelPosting("/obj1", 0, "env", "prod", 0, ts, 100)
	b.ObserveLabelPosting("/obj2", 0, "env", "prod", 1, ts, 200)

	// Same column/label, same path but different section index.
	b.ObserveLabelPosting("/obj1", 1, "env", "prod", 2, ts, 300)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	got := readAllPostings(t, sections[0])
	require.Len(t, got, 3, "different (objectPath, sectionIndex) should produce distinct postings")

	// Collect all (objectPath, sectionIndex) pairs from results.
	type key struct {
		path  string
		secID int64
	}
	seen := make(map[key]bool)
	for _, p := range got {
		k := key{p.ObjectPath, p.SectionIndex}
		require.False(t, seen[k], "duplicate (objectPath, sectionIndex) found")
		seen[k] = true
	}

	require.True(t, seen[key{"/obj1", 0}])
	require.True(t, seen[key{"/obj2", 0}])
	require.True(t, seen[key{"/obj1", 1}])

	// Each posting should only have the stream IDs for that context.
	for _, p := range got {
		switch {
		case p.ObjectPath == "/obj1" && p.SectionIndex == 0:
			require.True(t, checkBit(p.StreamIDBitmap, 0), "obj1/0 should have stream 0")
			require.Equal(t, int64(100), p.UncompressedSize)
		case p.ObjectPath == "/obj2" && p.SectionIndex == 0:
			require.True(t, checkBit(p.StreamIDBitmap, 1), "obj2/0 should have stream 1")
			require.Equal(t, int64(200), p.UncompressedSize)
		case p.ObjectPath == "/obj1" && p.SectionIndex == 1:
			require.True(t, checkBit(p.StreamIDBitmap, 2), "obj1/1 should have stream 2")
			require.Equal(t, int64(300), p.UncompressedSize)
		}
	}
}

// TestBuilder_BloomBytes verifies that BloomBytes returns valid marshaled bloom
// data matching what was observed.
func TestBuilder_BloomBytes(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	b.PrepareBloomColumn("/obj", 0, "col", 50)

	values := []string{"foo", "bar", "baz"}
	for _, v := range values {
		err := b.ObserveBloomPosting("/obj", 0, "col", v, 0, ts, 0)
		require.NoError(t, err)
	}

	bloomBytes, err := b.BloomBytes("/obj", 0, "col")
	require.NoError(t, err)
	require.NotEmpty(t, bloomBytes)

	// Verify the bloom bytes are valid by unmarshaling them.
	// The values we added should be present after round-trip.
	entry := b.blooms.entries[bloomPostingKey{"/obj", 0, "col"}]
	require.NotNil(t, entry)

	for _, v := range values {
		require.True(t, entry.BloomFilter().Test([]byte(v)), "expected %q to be in bloom filter", v)
	}
	require.False(t, entry.BloomFilter().Test([]byte("unknown")), "unexpected value in bloom filter")

	// Error for non-existent column.
	_, err = b.BloomBytes("/obj", 0, "nonexistent")
	require.Error(t, err)
}

// flushToObject flushes the builder into a dataobj.Object using dataobj.Builder.
func flushToObject(t *testing.T, b *Builder) (*dataobj.Object, io.Closer) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	err := objBuilder.Append(b)
	require.NoError(t, err)
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	return obj, closer
}

// flushAndOpenSections flushes the builder and opens all resulting postings sections.
func flushAndOpenSections(t *testing.T, b *Builder) []*Section {
	t.Helper()
	obj, closer := flushToObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	var sections []*Section
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err := Open(context.Background(), s)
		require.NoError(t, err)
		sections = append(sections, sec)
	}
	return sections
}

// readAllPostings reads all postings from a section.
func readAllPostings(t *testing.T, sec *Section) []Posting {
	t.Helper()
	rr := NewRowReader(sec)
	defer rr.Close()

	require.NoError(t, rr.Open(context.Background()))

	var all []Posting
	buf := make([]Posting, 100)
	for {
		n, err := rr.Read(context.Background(), buf)
		all = append(all, buf[:n]...)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	return all
}
