package encoding

import (
	"context"

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

		// StreamsDecoder returns a decoder for a streams section. The section is
		// not checked for type until the decoder is used.
		//
		// Sections where [filemd.SectionLayout] are defined are prevented from
		// reading outside of their layout.
		StreamsDecoder(metadata *filemd.Metadata, section *filemd.SectionInfo) StreamsDecoder

		// LogsDecoder returns a decoder for a logs section. The section is not
		// checked for type until the decoder is used.
		//
		// Sections where [filemd.SectionLayout] are defined are prevented from
		// reading outside of their layout.
		LogsDecoder(metadata *filemd.Metadata, section *filemd.SectionInfo) LogsDecoder
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
