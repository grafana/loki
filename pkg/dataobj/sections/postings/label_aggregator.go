package postings

import (
	"fmt"

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
	appendedKeys  map[labelPostingKey]struct{}
	estimatedSize int
}

// newLabelAggregator creates a new labelAggregator.
func newLabelAggregator() *labelAggregator {
	return &labelAggregator{
		entries:      make(map[labelPostingKey]*labelPostingEntry),
		appendedKeys: make(map[labelPostingKey]struct{}),
	}
}

// Observe records a single observation of a label posting. Returns an error if
// the key was previously added via AppendEntry.
func (a *labelAggregator) Observe(obs LabelObservation) error {
	key := labelPostingKey{
		objectPath:   obs.ObjectPath,
		sectionIndex: obs.SectionIndex,
		columnName:   obs.ColumnName,
		labelValue:   obs.LabelValue,
	}

	if _, ok := a.appendedKeys[key]; ok {
		return fmt.Errorf("label posting key %v already added via AppendLabelEntry", key)
	}

	tsNano := obs.Timestamp.UnixNano()

	entry, ok := a.entries[key]
	if !ok {
		entry = &labelPostingEntry{
			ObjectPath:   obs.ObjectPath,
			SectionIndex: obs.SectionIndex,
			ColumnName:   obs.ColumnName,
			LabelValue:   obs.LabelValue,
			bitmap:       memory.NewBitmap(nil, 0),
			MinTimestamp: tsNano,
			MaxTimestamp: tsNano,
		}
		a.entries[key] = entry

		// Track size for new entry: 5 int64 fields + string sizes
		a.estimatedSize += 5*8 + len(obs.ObjectPath) + len(obs.ColumnName) + len(obs.LabelValue)
	}

	// Grow bitmap if needed and set the bit for this stream ID.
	if int(obs.StreamID) >= entry.bitmap.Len() {
		prevLen := entry.bitmap.Len()
		entry.bitmap.Resize(int(obs.StreamID) + 1)
		// Track bitmap growth in estimated size.
		newBytes := (entry.bitmap.Len() + 7) / 8
		oldBytes := (prevLen + 7) / 8
		a.estimatedSize += newBytes - oldBytes
	}
	entry.bitmap.Set(int(obs.StreamID), true)

	if tsNano < entry.MinTimestamp {
		entry.MinTimestamp = tsNano
	}
	if tsNano > entry.MaxTimestamp {
		entry.MaxTimestamp = tsNano
	}
	entry.UncompressedSize += obs.UncompressedSize

	return nil
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

// AppendEntry appends a pre-built label posting entry directly to the
// aggregator, bypassing the observation aggregation path. Returns an error if
// an entry with the same key already exists.
func (a *labelAggregator) AppendEntry(entry *labelPostingEntry) error {
	key := labelPostingKey{
		objectPath:   entry.ObjectPath,
		sectionIndex: entry.SectionIndex,
		columnName:   entry.ColumnName,
		labelValue:   entry.LabelValue,
	}

	if _, ok := a.entries[key]; ok {
		return fmt.Errorf("label posting entry with key (%q, %d, %q, %q) already exists", entry.ObjectPath, entry.SectionIndex, entry.ColumnName, entry.LabelValue)
	}

	a.entries[key] = entry
	a.appendedKeys[key] = struct{}{}

	// Track size for appended entry: 5 int64 fields + string sizes + bitmap bytes
	bitmapBytes := entry.BitmapBytes()
	a.estimatedSize += 5*8 + len(entry.ObjectPath) + len(entry.ColumnName) + len(entry.LabelValue) + len(bitmapBytes)

	return nil
}

// Reset clears all accumulated state.
func (a *labelAggregator) Reset() {
	a.entries = make(map[labelPostingKey]*labelPostingEntry)
	a.appendedKeys = make(map[labelPostingKey]struct{})
	a.estimatedSize = 0
}
