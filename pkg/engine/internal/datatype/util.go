package datatype

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	ColumnMetadataBuiltinMessage   = ColumnMetadata(types.ColumnTypeBuiltin, Loki.String)
	ColumnMetadataBuiltinTimestamp = ColumnMetadata(types.ColumnTypeBuiltin, Loki.Timestamp)
)

func ColumnMetadata(ct types.ColumnType, dt DataType) arrow.Metadata {
	return arrow.NewMetadata(
		[]string{types.MetadataKeyColumnType, types.MetadataKeyColumnDataType},
		[]string{ct.String(), dt.String()},
	)
}
