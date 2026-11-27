package postings

import (
	"cmp"
)

// CompareRows defines the physical row order of a postings section and the
// merge order across sections. It reports whether row [a] sorts before (<0),
// after (>0), or equal to (0) row [b].
func CompareRows(a, b Row) int {
	return cmp.Or(
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.ColumnName, b.ColumnName),
		cmp.Compare(a.LabelValue, b.LabelValue),
		cmp.Compare(a.MinTimestamp, b.MinTimestamp),
		cmp.Compare(a.MaxTimestamp, b.MaxTimestamp),
		cmp.Compare(a.ObjectPath, b.ObjectPath),
		cmp.Compare(a.SectionIndex, b.SectionIndex),
	)
}

// Row is the decoded per-row representation of a postings section,
// covering both Label and Bloom kinds.
type Row struct {
	Kind             PostingKind // KindLabel or KindBloom
	ObjectPath       string
	ShardBuckets     int64
	SectionIndex     int64
	ColumnName       string
	LabelValue       string // empty for KindBloom rows
	StreamIDBitmap   []byte
	BloomFilter      []byte // nil for KindLabel rows
	UncompressedSize int64
	MinTimestamp     int64 // unix nanos
	MaxTimestamp     int64 // unix nanos
	MinShardBucket   uint32
	MaxShardBucket   uint32
}

// LabelEntry converts the Row to a [LabelEntry]. The caller should only call
// this when Row.Kind == KindLabel.
func (r Row) LabelEntry() LabelEntry {
	return LabelEntry{
		ObjectPath:       r.ObjectPath,
		ShardBuckets:     r.ShardBuckets,
		SectionIndex:     r.SectionIndex,
		ColumnName:       r.ColumnName,
		LabelValue:       r.LabelValue,
		StreamIDBitmap:   r.StreamIDBitmap,
		MinTimestamp:     r.MinTimestamp,
		MaxTimestamp:     r.MaxTimestamp,
		UncompressedSize: r.UncompressedSize,
		MinShardBucket:   r.MinShardBucket,
		MaxShardBucket:   r.MaxShardBucket,
	}
}

// BloomEntry converts the Row to a [BloomEntry]. The caller should only call
// this when Row.Kind == KindBloom.
func (r Row) BloomEntry() BloomEntry {
	return BloomEntry{
		ObjectPath:       r.ObjectPath,
		ShardBuckets:     r.ShardBuckets,
		SectionIndex:     r.SectionIndex,
		ColumnName:       r.ColumnName,
		BloomFilter:      r.BloomFilter,
		StreamIDBitmap:   r.StreamIDBitmap,
		MinTimestamp:     r.MinTimestamp,
		MaxTimestamp:     r.MaxTimestamp,
		UncompressedSize: r.UncompressedSize,
	}
}

// Row converts the LabelEntry to a [Row] with Kind set to [KindLabel]. It is
// the inverse of [Row.LabelEntry].
func (e LabelEntry) Row() Row {
	return Row{
		Kind:             KindLabel,
		ObjectPath:       e.ObjectPath,
		ShardBuckets:     e.ShardBuckets,
		SectionIndex:     e.SectionIndex,
		ColumnName:       e.ColumnName,
		LabelValue:       e.LabelValue,
		StreamIDBitmap:   e.StreamIDBitmap,
		MinTimestamp:     e.MinTimestamp,
		MaxTimestamp:     e.MaxTimestamp,
		UncompressedSize: e.UncompressedSize,
		MinShardBucket:   e.MinShardBucket,
		MaxShardBucket:   e.MaxShardBucket,
	}
}

// Row converts the BloomEntry to a [Row] with Kind set to [KindBloom]. It is
// the inverse of [Row.BloomEntry].
func (e BloomEntry) Row() Row {
	return Row{
		Kind:             KindBloom,
		ObjectPath:       e.ObjectPath,
		ShardBuckets:     e.ShardBuckets,
		SectionIndex:     e.SectionIndex,
		ColumnName:       e.ColumnName,
		BloomFilter:      e.BloomFilter,
		StreamIDBitmap:   e.StreamIDBitmap,
		MinTimestamp:     e.MinTimestamp,
		MaxTimestamp:     e.MaxTimestamp,
		UncompressedSize: e.UncompressedSize,
	}
}
