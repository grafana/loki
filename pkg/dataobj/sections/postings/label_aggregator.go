package postings

import (
	"time"

	"github.com/grafana/loki/v3/pkg/memory"
)

// labelPostingKey is the unique key for a label posting aggregation entry.
type labelPostingKey struct {
	objectPath   string
	sectionIndex int64
	columnName   string
	labelValue   string
}

// labelPostingEntry holds the aggregated state for a single label posting.
type labelPostingEntry struct {
	ObjectPath       string
	SectionIndex     int64
	ColumnName       string
	LabelValue       string
	bitmap           memory.Bitmap
	MinTimestamp     int64
	MaxTimestamp     int64
	UncompressedSize int64
}

// BitmapBytes returns the raw bytes of the bitmap trimmed to the logical size
// (ceil(Len()/8) bytes). Offset is always 0 for bitmaps built with Resize/Set.
func (e *labelPostingEntry) BitmapBytes() []byte {
	data, _ := e.bitmap.Bytes()
	n := (e.bitmap.Len() + 7) / 8
	if n > len(data) {
		n = len(data)
	}
	return data[:n]
}

// labelAggregator aggregates label posting observations incrementally.
// It is NOT goroutine-safe; callers must synchronize if needed.
type labelAggregator struct {
	entries       map[labelPostingKey]*labelPostingEntry
	maxStreamID   int64
	estimatedSize int
}

// newLabelAggregator creates a new labelAggregator.
func newLabelAggregator() *labelAggregator {
	return &labelAggregator{
		entries: make(map[labelPostingKey]*labelPostingEntry),
	}
}

// Observe records a single observation of a label posting.
func (a *labelAggregator) Observe(objectPath string, sectionIndex int64, columnName, labelValue string, streamID int64, ts time.Time, uncompressedSize int64) {
	key := labelPostingKey{
		objectPath:   objectPath,
		sectionIndex: sectionIndex,
		columnName:   columnName,
		labelValue:   labelValue,
	}

	entry, ok := a.entries[key]
	if !ok {
		entry = &labelPostingEntry{
			ObjectPath:   objectPath,
			SectionIndex: sectionIndex,
			ColumnName:   columnName,
			LabelValue:   labelValue,
			bitmap:       memory.NewBitmap(nil, 0),
			MinTimestamp: ts.UnixNano(),
			MaxTimestamp: ts.UnixNano(),
		}
		a.entries[key] = entry

		// Track size for new entry: 5 int64 fields + string sizes
		a.estimatedSize += 5*8 + len(objectPath) + len(columnName) + len(labelValue)
	}

	// Grow bitmap if needed and set the bit for this stream ID.
	if int(streamID) >= entry.bitmap.Len() {
		prevLen := entry.bitmap.Len()
		entry.bitmap.Resize(int(streamID) + 1)
		// Track bitmap growth in estimated size.
		newBytes := (entry.bitmap.Len() + 7) / 8
		oldBytes := (prevLen + 7) / 8
		a.estimatedSize += newBytes - oldBytes
	}
	entry.bitmap.Set(int(streamID), true)

	tsNano := ts.UnixNano()
	if tsNano < entry.MinTimestamp {
		entry.MinTimestamp = tsNano
	}
	if tsNano > entry.MaxTimestamp {
		entry.MaxTimestamp = tsNano
	}
	entry.UncompressedSize += uncompressedSize

	if streamID > a.maxStreamID {
		a.maxStreamID = streamID
	}
}

// Entries returns all aggregated entries. Bitmap normalization (padding to
// equal length) is NOT done here; the caller (columnarEncode) handles it
// across both label and bloom entries.
func (a *labelAggregator) Entries() []*labelPostingEntry {
	if len(a.entries) == 0 {
		return nil
	}

	result := make([]*labelPostingEntry, 0, len(a.entries))
	for _, entry := range a.entries {
		result = append(result, entry)
	}
	return result
}

// EstimatedSize returns an O(1) estimate of the encoded size of all
// accumulated observations in bytes.
func (a *labelAggregator) EstimatedSize() int {
	return a.estimatedSize
}

// Reset clears all accumulated state.
func (a *labelAggregator) Reset() {
	a.entries = make(map[labelPostingKey]*labelPostingEntry)
	a.maxStreamID = 0
	a.estimatedSize = 0
}
