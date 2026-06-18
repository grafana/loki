package postings

// SectionRef is a reference into a logs object's section, scoped to a single
// stream within that section. It is the resolved output of any metastore index
// reader (postings or legacy streams+pointers). Fields a given source format
// cannot supply are left at their zero value; MinTimestamp/MaxTimestamp use 0
// to mean "unknown" (callers map 0 to a zero time, not the Unix epoch).
type SectionRef struct {
	ObjectPath       string
	SectionIndex     int64
	StreamID         int64
	MinTimestamp     int64 // unix nanos; 0 means unknown
	MaxTimestamp     int64 // unix nanos; 0 means unknown
	RowCount         int64 // log-line count; 0 for the postings path
	UncompressedSize int64 // 0 when unknown
	AmbiguousLabels  []string
}
