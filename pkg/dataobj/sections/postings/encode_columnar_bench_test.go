package postings

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// discardSectionWriter is a no-op [dataobj.SectionWriter] that counts bytes so
// benchmarks measure encoding cost without an object builder or I/O.
type discardSectionWriter struct{}

func (discardSectionWriter) WriteSection(_ *dataobj.WriteSectionOptions, data, metadata []byte) (int64, error) {
	return int64(len(data) + len(metadata)), nil
}

// BenchmarkEncodeLabelEntries encodes a section of label postings with a
// realistic skew in stream-ID bitmap sizes: a few "hot" label values match many
// streams (large bitmaps) while most match few (tiny bitmaps).
//
// The encoder used to zero-pad every row's bitmap to the section's longest
// bitmap, so a single hot label forced an allocate-and-zero of that maximum
// length for every row — dominating both compaction CPU (memclr) and index
// object size. Bitmaps are now stored at their natural length. Run across
// branches with benchstat; the win shows in both ns/op and B/op.
func BenchmarkEncodeLabelEntries(b *testing.B) {
	const (
		numRows  = 20_000
		hotEvery = 500 // one hot label per this many rows
	)
	hot := bytes.Repeat([]byte{0xFF}, 8<<10) // 8 KiB: a label present in many streams
	cold := []byte{0x01}                     // a label present in a single stream

	entries := make([]LabelEntry, numRows)
	for i := range entries {
		bitmap := cold
		if i%hotEvery == 0 {
			bitmap = hot
		}
		entries[i] = LabelEntry{
			ObjectPath:       "logs/tenant/obj",
			SectionIndex:     0,
			ColumnName:       "service_name",
			LabelValue:       fmt.Sprintf("svc-%06d", i),
			StreamIDBitmap:   bitmap,
			MinTimestamp:     int64(i),
			MaxTimestamp:     int64(i) + 1,
			UncompressedSize: 100,
		}
	}
	sortLabelEntries(entries)

	// Production-like page and section sizing (2KB pages, 2MB sections).
	enc := newPostingsEncoder(2<<10, 0, 2<<20)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := enc.encodeLabelEntries(discardSectionWriter{}, "tenant", entries); err != nil {
			b.Fatal(err)
		}
	}
}
