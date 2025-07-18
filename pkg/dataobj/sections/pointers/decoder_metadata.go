package pointers

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/pointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// decodePointersMetadata decodes pointers section metadata from r.
func decodePointersMetadata(r streamio.Reader) (*pointersmd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read pointers section format version: %w", err)
	} else if gotVersion != pointersFormatVersion {
		return nil, fmt.Errorf("unexpected pointers section format version: got=%d want=%d", gotVersion, pointersFormatVersion)
	}

	var md pointersmd.Metadata
	if err := protocodec.Decode(r, &md); err != nil {
		return nil, fmt.Errorf("pointers section metadata: %w", err)
	}
	return &md, nil
}

// decodePointersColumnMetadata decodes pointers column metadata from r.
func decodePointersColumnMetadata(r streamio.Reader) (*pointersmd.ColumnMetadata, error) {
	var metadata pointersmd.ColumnMetadata
	if err := protocodec.Decode(r, &metadata); err != nil {
		return nil, fmt.Errorf("pointers column metadata: %w", err)
	}
	return &metadata, nil
}
