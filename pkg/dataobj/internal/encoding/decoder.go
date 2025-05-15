package encoding

import (
	"context"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

// Decoders. To cleanly separate the APIs per section, section-specific
// Decoders should be created and returned by the top-level [Decoder]
// interface.
type (
	// A Decoder decodes a data object.
	Decoder interface {
		// Metadata returns the top-level metadata of the data object.
		Metadata(ctx context.Context) (*filemd.Metadata, error)

		// SectionReader returns a reader for a section.
		//
		// Sections where [filemd.SectionLayout] are defined are prevented from
		// reading outside of their layout.
		SectionReader(metadata *filemd.Metadata, section *filemd.SectionInfo) SectionReader
	}

	// A SectionReader allows regions of a section to be read for decoding.
	SectionReader interface {
		// Type returns the type of section that is being read.
		Type() (SectionType, error)

		// DataRange opens a reader of length bytes from the data region of a
		// section. The offset argument determines where in the data region reading
		// should start.
		//
		// DataRange returns an error if the read fails or if offset+length goes
		// beyond the readable data region. The returned reader is only valid as long
		// as the provided ctx is not canceled.
		//
		// Implementations of SectionReader should interpret offset as relative to
		// the start of the data object if the data object does not explicitly
		// specify a data region. This is only relevant to older versions of data
		// objects where sections used absolute offsets and the data region was
		// implicitly the entire file.
		DataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)

		// Metadata opens a reader to the entire metadata region of a section.
		// Metadata returns an error if the read fails. The returned reader is only
		// valid as long as the provided ctx is not canceled.
		Metadata(ctx context.Context) (io.ReadCloser, error)
	}
)
