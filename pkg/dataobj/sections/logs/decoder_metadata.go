package logs

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// decodeLogsMetadata decodes logs section metadata from r.
func decodeLogsMetadata(r streamio.Reader) (*logsmd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read logs section format version: %w", err)
	} else if gotVersion != logsFormatVersion {
		return nil, fmt.Errorf("unexpected logs section format version: got=%d want=%d", gotVersion, logsFormatVersion)
	}

	var md logsmd.Metadata
	if err := protocodec.Decode(r, &md); err != nil {
		return nil, fmt.Errorf("streams section metadata: %w", err)
	}
	return &md, nil
}

// decodeLogsColumnMetadata decodes logs column metadata from r.
func decodeLogsColumnMetadata(r streamio.Reader) (*logsmd.ColumnMetadata, error) {
	var metadata logsmd.ColumnMetadata
	if err := protocodec.Decode(r, &metadata); err != nil {
		return nil, fmt.Errorf("streams column metadata: %w", err)
	}
	return &metadata, nil
}
