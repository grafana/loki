package encoding

import (
	"context"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
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

		// LogsDecoder returns a decoder for a logs section. The section is not
		// checked for type until the decoder is used.
		//
		// Sections where [filemd.SectionLayout] are defined are prevented from
		// reading outside of their layout.
		LogsDecoder(metadata *filemd.Metadata, section *filemd.SectionInfo) LogsDecoder
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

	// StreamsDecoder supports decoding data of a streams section.
	StreamsDecoder interface {
		// Columns describes the set of columns the section. Columns returns an
		// error if the section associated with the StreamsDecoder is not a valid
		// streams section.
		Columns(ctx context.Context) ([]*streamsmd.ColumnDesc, error)

		// Pages retrieves the set of pages for the provided columns. The order of
		// page lists emitted by the sequence matches the order of columns
		// provided: the first page list corresponds to the first column, and so
		// on.
		Pages(ctx context.Context, columns []*streamsmd.ColumnDesc) result.Seq[[]*streamsmd.PageDesc]

		// ReadPages reads the provided set of pages, iterating over their data
		// matching the argument order. If an error is encountered while retrieving
		// pages, an error is emitted and iteration stops.
		ReadPages(ctx context.Context, pages []*streamsmd.PageDesc) result.Seq[dataset.PageData]
	}

	// LogsDecoder supports decoding data within a logs section.
	LogsDecoder interface {
		// Columns describes the set of columns in the provided section. Columns
		// returns an error if the section associated with the LogsDecoder is not a
		// valid logs section.
		Columns(ctx context.Context) ([]*logsmd.ColumnDesc, error)

		// Pages retrieves the set of pages for the provided columns. The order of
		// page lists emitted by the sequence matches the order of columns
		// provided: the first page list corresponds to the first column, and so
		// on.
		Pages(ctx context.Context, columns []*logsmd.ColumnDesc) result.Seq[[]*logsmd.PageDesc]

		// ReadPages reads the provided set of pages, iterating over their data
		// matching the argument order. If an error is encountered while retrieving
		// pages, an error is emitted and iteration stops.
		ReadPages(ctx context.Context, pages []*logsmd.PageDesc) result.Seq[dataset.PageData]
	}
)
