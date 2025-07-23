package indexpointers

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/indexpointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// decodeIndexPointersMetadata decodes indexpointers section metadata from r.
func decodeIndexPointersMetadata(r streamio.Reader) (*indexpointersmd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read indexpointers section format version: %w", err)
	} else if gotVersion != indexPointersFormatVersion {
		return nil, fmt.Errorf("unexpected indexpointers section format version: got=%d want=%d", gotVersion, indexPointersFormatVersion)
	}

	var md indexpointersmd.Metadata
	if err := protocodec.Decode(r, &md); err != nil {
		return nil, fmt.Errorf("indexpointers section metadata: %w", err)
	}
	return &md, nil
}

// decodeIndexPointersColumnMetadata decodes indexpointers column metadata from r.
func decodeIndexPointersColumnMetadata(r streamio.Reader) (*indexpointersmd.ColumnMetadata, error) {
	var metadata indexpointersmd.ColumnMetadata
	if err := protocodec.Decode(r, &metadata); err != nil {
		return nil, fmt.Errorf("indexpointers column metadata: %w", err)
	}
	return &metadata, nil
}
