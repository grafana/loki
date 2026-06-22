package postings

// SectionResult is the resolved output for a single section: the set of matching
// streams (as a stream-ID bitmap, bit position = stream ID), a conservative
// section-wide timestamp envelope, and the section-level set of ambiguous label
// names (equal-predicate names that are also stream labels in this section).
type SectionResult struct {
	ObjectPath     string
	SectionIndex   int64
	StreamBitmap   []byte // bit i set => stream i matches; expand via memory.BitmapFrom(...).IterValues(true)
	MinTimestamp   int64  // unix nanos; 0 means unknown
	MaxTimestamp   int64  // unix nanos; 0 means unknown
	AmbiguousNames []string
}
