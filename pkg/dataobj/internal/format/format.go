// Package format holds the constants of the data object file format that more than one package of
// pkg/dataobj needs, and the functions that interpret them. It imports no other package, so any
// package of pkg/dataobj can import it without an import cycle.
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

// The format versions of the columnar sections: the sections that share the encoding in
// pkg/dataobj/sections/internal/columnar. A section of a type version has the format version of
// the same number, except a logs section of version 3, which has format version 2.
const (
	// ColumnarVersionDescriptorChecksum keeps the CRC32C checksum of each page in its page
	// descriptor.
	ColumnarVersionDescriptorChecksum uint32 = 2

	// ColumnarVersionPageChecksum keeps the CRC32C checksum of each page at the end of the page.
	ColumnarVersionPageChecksum uint32 = 4
)

// ColumnarPageHasChecksum returns true if the pages of a columnar section of the given format
// version end with their checksum. Every version from [ColumnarVersionPageChecksum] on has it. The
// zero value is not a format version. It means the checksum is in the descriptor.
func ColumnarPageHasChecksum(version uint32) bool {
	return version >= ColumnarVersionPageChecksum
}

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
