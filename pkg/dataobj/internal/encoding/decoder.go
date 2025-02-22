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
		// Sections returns the list of sections within a data object.
		Sections(ctx context.Context) ([]*filemd.SectionInfo, error)

		// StreamsDecoder returns a decoder for streams sections.
		StreamsDecoder() StreamsDecoder

		// LogsDecoder returns a decoder for logs sections.
		LogsDecoder() LogsDecoder
	}

	// StreamsDecoder supports decoding data within a streams section.
	StreamsDecoder interface {
		// Columns describes the set of columns in the provided section.
		Columns(ctx context.Context, section *filemd.SectionInfo) ([]*streamsmd.ColumnDesc, error)

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
		// Columns describes the set of columns in the provided section.
		Columns(ctx context.Context, section *filemd.SectionInfo) ([]*logsmd.ColumnDesc, error)

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
