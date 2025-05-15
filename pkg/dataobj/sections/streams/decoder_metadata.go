package streams

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// decodeStreamsMetadata decodes stream section metadata from r.
func decodeStreamsMetadata(r streamio.Reader) (*streamsmd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read streams section format version: %w", err)
	} else if gotVersion != streamsFormatVersion {
		return nil, fmt.Errorf("unexpected streams section format version: got=%d want=%d", gotVersion, streamsFormatVersion)
	}

	var md streamsmd.Metadata
	if err := encoding.DecodeProto(r, &md); err != nil {
		return nil, fmt.Errorf("streams section metadata: %w", err)
	}
	return &md, nil
}

// decodeStreamsColumnMetadata decodes stream column metadata from r.
func decodeStreamsColumnMetadata(r streamio.Reader) (*streamsmd.ColumnMetadata, error) {
	var metadata streamsmd.ColumnMetadata
	if err := encoding.DecodeProto(r, &metadata); err != nil {
		return nil, fmt.Errorf("streams column metadata: %w", err)
	}
	return &metadata, nil
}
