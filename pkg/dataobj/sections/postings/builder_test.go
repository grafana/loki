package postings

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/memory"
)

// TestBuilder_Empty verifies that an empty builder produces no sections.
func TestBuilder_Empty(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)
	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Empty(t, sections, "empty builder should produce no sections")
}

// TestBuilder_LabelPostingRoundTrip verifies a label posting round-trips correctly.
func TestBuilder_LabelPostingRoundTrip(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bitmap := makeBitmap(t, 3, 7, 15)
	input := Posting{
		Kind:             KindLabel,
		ObjectPath:       "/tenant/abc/obj1",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "value1",
		BloomFilter:      nil,
		StreamIDBitmap:   bitmap,
		UncompressedSize: 4096,
		MinTimestamp:     1000,
		MaxTimestamp:     2000,
	}
	b.Append(input)

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 1)

	p := got[0]
	require.Equal(t, KindLabel, p.Kind)
	require.Equal(t, "/tenant/abc/obj1", p.ObjectPath)
	require.Equal(t, int64(0), p.SectionIndex)
	require.Equal(t, "env", p.ColumnName)
	require.Equal(t, "value1", p.LabelValue)
	require.Nil(t, p.BloomFilter)
	require.Equal(t, bitmap, p.StreamIDBitmap)
	require.Equal(t, int64(4096), p.UncompressedSize)
	require.Equal(t, int64(1000), p.MinTimestamp)
	require.Equal(t, int64(2000), p.MaxTimestamp)
}

// TestBuilder_BloomPostingRoundTrip verifies a bloom posting round-trips correctly.
func TestBuilder_BloomPostingRoundTrip(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bitmap := makeBitmap(t, 0, 2, 8)
	bloomFilter := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	input := Posting{
		Kind:             KindBloom,
		ObjectPath:       "/tenant/abc/obj2",
		SectionIndex:     1,
		ColumnName:       "service_name",
		BloomFilter:      bloomFilter,
		StreamIDBitmap:   bitmap,
		UncompressedSize: 8192,
		MinTimestamp:     500,
		MaxTimestamp:     1500,
	}
	b.Append(input)

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 1)

	p := got[0]
	require.Equal(t, KindBloom, p.Kind)
	require.Equal(t, "/tenant/abc/obj2", p.ObjectPath)
	require.Equal(t, int64(1), p.SectionIndex)
	require.Equal(t, "service_name", p.ColumnName)
	require.Empty(t, p.LabelValue)
	require.Equal(t, bloomFilter, p.BloomFilter)
	require.Equal(t, bitmap, p.StreamIDBitmap)
	require.Equal(t, int64(8192), p.UncompressedSize)
	require.Equal(t, int64(500), p.MinTimestamp)
	require.Equal(t, int64(1500), p.MaxTimestamp)
}

// TestBuilder_MixedPostings verifies both Bloom and Label postings work together.
func TestBuilder_MixedPostings(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bloom := Posting{
		Kind:           KindBloom,
		ObjectPath:     "/obj1",
		ColumnName:     "col_a",
		BloomFilter:    []byte{0x01, 0x02},
		StreamIDBitmap: makeBitmap(t, 0),
		MinTimestamp:   100,
		MaxTimestamp:   200,
	}
	label := Posting{
		Kind:           KindLabel,
		ObjectPath:     "/obj2",
		ColumnName:     "col_b",
		LabelValue:     "myval",
		BloomFilter:    nil,
		StreamIDBitmap: makeBitmap(t, 1, 3),
		MinTimestamp:   300,
		MaxTimestamp:   400,
	}

	b.Append(label) // Append out of sort order
	b.Append(bloom)

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 2)

	// Bloom (KindBloom=0) sorts before Label (KindLabel=1).
	require.Equal(t, KindBloom, got[0].Kind)
	require.Empty(t, got[0].LabelValue)
	require.NotNil(t, got[0].BloomFilter)

	require.Equal(t, KindLabel, got[1].Kind)
	require.NotEmpty(t, got[1].LabelValue)
	require.Nil(t, got[1].BloomFilter)
}

// TestBuilder_SortOrder verifies the sort order: [Kind, ColumnName, LabelValue, MinTimestamp].
func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bitmapBytes := makeBitmap(t, 0)

	// Bloom entries (Kind=0) should come before Label entries (Kind=1) for
	// same column. Within Bloom, sort by ColumnName then MinTimestamp.
	// Nil LabelValue for Bloom sorts as "" (before any label value string).
	postings := []Posting{
		{Kind: KindLabel, ColumnName: "col_a", LabelValue: "beta", MinTimestamp: 200, StreamIDBitmap: bitmapBytes},
		{Kind: KindLabel, ColumnName: "col_a", LabelValue: "alpha", MinTimestamp: 100, StreamIDBitmap: bitmapBytes},
		{Kind: KindBloom, ColumnName: "col_a", BloomFilter: []byte{0x01}, MinTimestamp: 50, StreamIDBitmap: bitmapBytes},
		{Kind: KindLabel, ColumnName: "col_a", LabelValue: "alpha", MinTimestamp: 50, StreamIDBitmap: bitmapBytes},
		{Kind: KindBloom, ColumnName: "col_b", BloomFilter: []byte{0x02}, MinTimestamp: 10, StreamIDBitmap: bitmapBytes},
	}

	for _, p := range postings {
		b.Append(p)
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 5)

	// Expected order:
	// 0: KindBloom, col_a, nil, ts=50
	// 1: KindBloom, col_b, nil, ts=10
	// 2: KindLabel, col_a, alpha, ts=50
	// 3: KindLabel, col_a, alpha, ts=100
	// 4: KindLabel, col_a, beta, ts=200
	require.Equal(t, KindBloom, got[0].Kind)
	require.Equal(t, "col_a", got[0].ColumnName)
	require.Empty(t, got[0].LabelValue)

	require.Equal(t, KindBloom, got[1].Kind)
	require.Equal(t, "col_b", got[1].ColumnName)
	require.Empty(t, got[1].LabelValue)

	require.Equal(t, KindLabel, got[2].Kind)
	require.Equal(t, "col_a", got[2].ColumnName)
	require.Equal(t, "alpha", got[2].LabelValue)
	require.Equal(t, int64(50), got[2].MinTimestamp)

	require.Equal(t, KindLabel, got[3].Kind)
	require.Equal(t, "col_a", got[3].ColumnName)
	require.Equal(t, "alpha", got[3].LabelValue)
	require.Equal(t, int64(100), got[3].MinTimestamp)

	require.Equal(t, KindLabel, got[4].Kind)
	require.Equal(t, "col_a", got[4].ColumnName)
	require.Equal(t, "beta", got[4].LabelValue)
}

// TestBuilder_NullableHandling verifies nullable column correctness.
func TestBuilder_NullableHandling(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bitmapBytes := makeBitmap(t, 0)

	b.Append(Posting{
		Kind: KindBloom, ColumnName: "col",
		BloomFilter:    []byte{0xAB},
		StreamIDBitmap: bitmapBytes,
	})
	b.Append(Posting{
		Kind: KindLabel, ColumnName: "col",
		LabelValue:     "val",
		StreamIDBitmap: bitmapBytes,
	})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
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
	b := NewBuilder(0, ColumnarEncoder)

	// Build bitmap with stream IDs 0, 3, 7.
	// Byte 0: bit 0 = 1 (stream 0), bit 3 = 1 (stream 3), bit 7 = 1 (stream 7).
	// Expected byte 0: 0b10001001 = 0x89
	var alloc memory.Allocator
	bmap := memory.NewBitmap(&alloc, 8)
	bmap.Resize(8)
	bmap.Set(0, true)
	bmap.Set(3, true)
	bmap.Set(7, true)
	bitmapData, _ := bmap.Bytes()
	bitmapBytes := make([]byte, 1)
	copy(bitmapBytes, bitmapData[:1])

	b.Append(Posting{
		Kind:           KindBloom,
		ColumnName:     "col",
		BloomFilter:    []byte{0x01},
		StreamIDBitmap: bitmapBytes,
	})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 1)

	// Verify LSB encoding: bit N at byte N/8, position N%8.
	bitmap := got[0].StreamIDBitmap
	require.NotEmpty(t, bitmap)

	checkBit := func(data []byte, n int) bool {
		byteIdx := n / 8
		bitPos := uint(n % 8)
		return (data[byteIdx]>>bitPos)&1 == 1
	}

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
	b := NewBuilder(0, ColumnarEncoder)

	short := []byte{0x01}            // 1 byte
	long := []byte{0x01, 0x02, 0x03} // 3 bytes

	b.Append(Posting{
		Kind:           KindLabel,
		ColumnName:     "col",
		LabelValue:     "a",
		StreamIDBitmap: short,
		MinTimestamp:   100,
	})
	b.Append(Posting{
		Kind:           KindLabel,
		ColumnName:     "col",
		LabelValue:     "b",
		StreamIDBitmap: long,
		MinTimestamp:   200,
	})

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 2)

	// All bitmaps should be the same length (3 bytes, the maximum).
	require.Len(t, got[0].StreamIDBitmap, 3, "short bitmap should be padded to max length")
	require.Len(t, got[1].StreamIDBitmap, 3, "long bitmap should remain at max length")

	// Short bitmap should be zero-padded.
	require.Equal(t, byte(0x01), got[0].StreamIDBitmap[0])
	require.Equal(t, byte(0x00), got[0].StreamIDBitmap[1])
	require.Equal(t, byte(0x00), got[0].StreamIDBitmap[2])

	// Long bitmap should be unchanged.
	require.Equal(t, long, got[1].StreamIDBitmap)
}

// TestBuilder_SectionSplitting verifies that a small targetSectionSize causes
// rows to be split across multiple sections.
func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small targetSectionSize to force splitting.
	b := NewBuilder(100, ColumnarEncoder)

	bitmapBytes := makeBitmap(t, 0)
	for i := range 6 {
		b.Append(Posting{
			Kind:           KindLabel,
			ColumnName:     "col",
			LabelValue:     "val",
			StreamIDBitmap: bitmapBytes,
			MinTimestamp:   int64(i * 100),
		})
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Greater(t, len(sections), 1, "expected multiple sections due to splitting")

	// Collect all rows across sections.
	var allPostings []Posting
	for _, sec := range sections {
		rr, err := NewRowReader(&sec)
		require.NoError(t, err)
		defer rr.Close()

		buf := make([]Posting, 10)
		for {
			n, err := rr.Read(context.Background(), buf)
			allPostings = append(allPostings, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
	}
	require.Len(t, allPostings, 6)

	// Rows should be in sorted order across sections.
	for i := 1; i < len(allPostings); i++ {
		require.LessOrEqual(t, allPostings[i-1].MinTimestamp, allPostings[i].MinTimestamp)
	}
}

// TestBuilder_AllBloom verifies that a builder with only Bloom postings works.
func TestBuilder_AllBloom(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bitmapBytes := makeBitmap(t, 0)
	for i := range 3 {
		b.Append(Posting{
			Kind:           KindBloom,
			ColumnName:     "col",
			BloomFilter:    []byte{byte(i)},
			StreamIDBitmap: bitmapBytes,
			MinTimestamp:   int64(i * 100),
		})
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 3)
	for _, p := range got {
		require.Equal(t, KindBloom, p.Kind)
		require.Empty(t, p.LabelValue)
		require.NotNil(t, p.BloomFilter)
	}
}

// TestBuilder_AllLabel verifies that a builder with only Label postings works.
func TestBuilder_AllLabel(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)

	bitmapBytes := makeBitmap(t, 0)
	for i := range 3 {
		lv := fmt.Sprintf("val%d", i)
		b.Append(Posting{
			Kind:           KindLabel,
			ColumnName:     "col",
			LabelValue:     lv,
			StreamIDBitmap: bitmapBytes,
			MinTimestamp:   int64(i * 100),
		})
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	got := readAllPostings(t, &sections[0])
	require.Len(t, got, 3)
	for _, p := range got {
		require.Equal(t, KindLabel, p.Kind)
		require.NotEmpty(t, p.LabelValue)
		require.Nil(t, p.BloomFilter)
	}
}

// TestBuilder_FlushResetsBuilder verifies that a flush resets the builder.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)
	b.Append(Posting{Kind: KindLabel, ColumnName: "col", LabelValue: "v", StreamIDBitmap: makeBitmap(t, 0)})

	_, err := b.Flush(context.Background())
	require.NoError(t, err)

	// After flush, builder should be empty.
	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Empty(t, sections)
}

// TestBuilder_Type verifies that the section type is correct.
func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(0, ColumnarEncoder)
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
	b := NewBuilder(0, ColumnarEncoder)

	bitmapBytes := makeBitmap(t, 0)
	// Append 5 rows.
	for i := range 5 {
		b.Append(Posting{
			Kind:           KindLabel,
			ColumnName:     "col",
			LabelValue:     fmt.Sprintf("val%d", i),
			StreamIDBitmap: bitmapBytes,
			MinTimestamp:   int64(i * 100),
		})
	}

	sections, err := b.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, sections, 1)

	rr, err := NewRowReader(&sections[0])
	require.NoError(t, err)
	defer rr.Close()

	// Read with a buffer smaller than the section row count. This must not
	// panic even though the underlying column reader returns all rows at once.
	buf := make([]Posting, 2)
	n, err := rr.Read(context.Background(), buf)
	require.NoError(t, err)
	require.Equal(t, 2, n, "should read exactly len(buf) rows")
}

// readAllPostings reads all postings from a section.
func readAllPostings(t *testing.T, sec *Section) []Posting {
	t.Helper()
	rr, err := NewRowReader(sec)
	require.NoError(t, err)
	defer rr.Close()

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

// makeBitmap creates a bitmap with the given stream IDs set, and returns its
// raw bytes (padded to a full byte boundary).
func makeBitmap(t *testing.T, streamIDs ...int) []byte {
	t.Helper()
	if len(streamIDs) == 0 {
		return []byte{0x00}
	}

	maxID := 0
	for _, id := range streamIDs {
		if id > maxID {
			maxID = id
		}
	}

	var alloc memory.Allocator
	bmap := memory.NewBitmap(&alloc, maxID+1)
	bmap.Resize(maxID + 1)
	for _, id := range streamIDs {
		bmap.Set(id, true)
	}

	data, _ := bmap.Bytes()
	numBytes := (maxID + 8) / 8
	result := make([]byte, numBytes)
	copy(result, data[:numBytes])
	return result
}
