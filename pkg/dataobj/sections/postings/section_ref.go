package postings

// SectionRef references one logs section: a (data object, section index) pair.
// A single physical postings section interleaves rows for many logs sections,
// and a stream-ID bit is only meaningful within one of them, so SectionRef is
// the key under which matching streams are accumulated.
type SectionRef struct {
	ObjectPath   string
	SectionIndex int64
}
