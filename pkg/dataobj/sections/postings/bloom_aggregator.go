package postings

import (
	"fmt"
	"math"

	"github.com/bits-and-blooms/bloom/v3"

	"github.com/grafana/loki/v3/pkg/memory"
)

// bloomPostingKey is the unique key for a bloom posting aggregation entry.
type bloomPostingKey struct {
	objectPath   string
	sectionIndex int64
	columnName   string
}

// bloomPostingEntry holds the aggregated state for a single bloom posting.
type bloomPostingEntry struct {
	ObjectPath       string
	SectionIndex     int64
	ColumnName       string
	bloomFilter      *bloom.BloomFilter
	bitmap           memory.Bitmap
	MinTimestamp     int64
	MaxTimestamp     int64
	UncompressedSize int64
}

// BloomFilter returns the bloom filter for this entry.
func (e *bloomPostingEntry) BloomFilter() *bloom.BloomFilter {
	return e.bloomFilter
}

// BloomBytes marshals the bloom filter to binary and returns the bytes.
func (e *bloomPostingEntry) BloomBytes() ([]byte, error) {
	b, err := e.bloomFilter.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshaling bloom filter: %w", err)
	}
	return b, nil
}

// BitmapBytes returns the raw bytes of the bitmap trimmed to the logical size
// (ceil(Len()/8) bytes). Offset is always 0 for bitmaps built with Resize/Set.
func (e *bloomPostingEntry) BitmapBytes() []byte {
	data, _ := e.bitmap.Bytes()
	n := (e.bitmap.Len() + 7) / 8
	if n > len(data) {
		n = len(data)
	}
	return data[:n]
}

// bloomAggregator aggregates bloom posting observations per metadata column.
// It is NOT goroutine-safe; callers must synchronize if needed.
type bloomAggregator struct {
	entries       map[bloomPostingKey]*bloomPostingEntry
	estimatedSize int
}

// newBloomAggregator creates a new bloomAggregator.
func newBloomAggregator() *bloomAggregator {
	return &bloomAggregator{
		entries: make(map[bloomPostingKey]*bloomPostingEntry),
	}
}

// PrepareColumn initializes the bloom filter for a specific column. Must be
// called before any Observe calls for the given (objectPath, sectionIndex,
// columnName) combination.
func (a *bloomAggregator) PrepareColumn(objectPath string, sectionIndex int64, columnName string, estimatedCardinality uint) {
	key := bloomPostingKey{
		objectPath:   objectPath,
		sectionIndex: sectionIndex,
		columnName:   columnName,
	}
	if _, ok := a.entries[key]; ok {
		// Already prepared; do not overwrite.
		return
	}

	entry := &bloomPostingEntry{
		ObjectPath:   objectPath,
		SectionIndex: sectionIndex,
		ColumnName:   columnName,
		bloomFilter:  bloom.NewWithEstimates(estimatedCardinality, 1.0/128.0),
		bitmap:       memory.NewBitmap(nil, 0),
		MinTimestamp: math.MaxInt64,
		MaxTimestamp: math.MinInt64,
	}
	a.entries[key] = entry

	// Track size estimate for new entry: 5 int64 fields + string sizes + bloom filter capacity.
	a.estimatedSize += 5*8 + len(objectPath) + len(columnName) + int(entry.bloomFilter.Cap()/8)
}

// Observe records a single observation for a bloom column. Returns an error if
// the column has not been prepared via PrepareColumn.
func (a *bloomAggregator) Observe(obs BloomObservation) error {
	key := bloomPostingKey{
		objectPath:   obs.ObjectPath,
		sectionIndex: obs.SectionIndex,
		columnName:   obs.ColumnName,
	}

	entry, ok := a.entries[key]
	if !ok {
		return fmt.Errorf("bloom column not prepared: objectPath=%q sectionIndex=%d columnName=%q", obs.ObjectPath, obs.SectionIndex, obs.ColumnName)
	}

	entry.bloomFilter.Add([]byte(obs.Value))

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

	tsNano := obs.Timestamp.UnixNano()
	if tsNano < entry.MinTimestamp {
		entry.MinTimestamp = tsNano
	}
	if tsNano > entry.MaxTimestamp {
		entry.MaxTimestamp = tsNano
	}
	entry.UncompressedSize += obs.UncompressedSize

	return nil
}

// Entries returns all aggregated entries converted to public type. Bitmap
// normalization (padding to equal length) is NOT done here; the caller
// (columnarEncode) handles it across both label and bloom entries.
// Returns an error if any bloom filter fails to marshal.
func (a *bloomAggregator) Entries() ([]BloomEntry, error) {
	if len(a.entries) == 0 {
		return nil, nil
	}

	result := make([]BloomEntry, 0, len(a.entries))
	for _, entry := range a.entries {
		bloomBytes, err := entry.BloomBytes()
		if err != nil {
			return nil, fmt.Errorf("marshaling bloom filter for %q section %d column %q: %w",
				entry.ObjectPath, entry.SectionIndex, entry.ColumnName, err)
		}
		result = append(result, BloomEntry{
			ObjectPath:       entry.ObjectPath,
			SectionIndex:     entry.SectionIndex,
			ColumnName:       entry.ColumnName,
			BloomFilter:      bloomBytes,
			StreamIDBitmap:   entry.BitmapBytes(),
			MinTimestamp:     entry.MinTimestamp,
			MaxTimestamp:     entry.MaxTimestamp,
			UncompressedSize: entry.UncompressedSize,
		})
	}
	return result, nil
}

// BloomBytes marshals and returns the bloom filter bytes for a specific column.
// Returns an error if the column has not been prepared.
func (a *bloomAggregator) BloomBytes(objectPath string, sectionIndex int64, columnName string) ([]byte, error) {
	key := bloomPostingKey{
		objectPath:   objectPath,
		sectionIndex: sectionIndex,
		columnName:   columnName,
	}
	entry, ok := a.entries[key]
	if !ok {
		return nil, fmt.Errorf("bloom column not prepared: objectPath=%q sectionIndex=%d columnName=%q", objectPath, sectionIndex, columnName)
	}
	return entry.BloomBytes()
}

// EstimatedSize returns an O(1) estimate of the encoded size of all
// accumulated observations in bytes.
func (a *bloomAggregator) EstimatedSize() int {
	return a.estimatedSize
}

// Reset clears all accumulated state including prepared columns.
func (a *bloomAggregator) Reset() {
	a.entries = make(map[bloomPostingKey]*bloomPostingEntry)
	a.estimatedSize = 0
}
