// Package format holds the constants of the data object file format that more than one package of
// pkg/dataobj needs. It imports no other package, so any package of pkg/dataobj can import it
// without an import cycle.
package format

const (
	// FileMagic starts the header of a data object. It also ends the object.
	FileMagic = "DOBJ"

	// HeaderSize is the size of the header: FileMagic and the little-endian size of the file
	// metadata, which take four bytes each. The file metadata follows the header.
	HeaderSize = 8

	// FileFormatVersion is the version of the file format. The file metadata starts with it, as a
	// uvarint.
	FileFormatVersion = 1

	// SectionNamespace is the namespace of the section types that Loki defines.
	SectionNamespace = "github.com/grafana/loki"
)

// The kinds of the section types that Loki defines. A section type is a namespace, a kind and a
// version.
const (
	IndexPointersSectionKind = "indexpointers"
	LogsSectionKind          = "logs"
	PostingsSectionKind      = "postings"
	StatsSectionKind         = "stats"
	StreamsSectionKind       = "streams"
	WatermarksSectionKind    = "watermarks"
)

// The logical types of the columns of a logs section.
const (
	LogsColumnStreamID  = "stream_id"
	LogsColumnTimestamp = "timestamp"
	LogsColumnMetadata  = "metadata"
	LogsColumnMessage   = "message"
)
