package types //nolint:revive

import (
	"github.com/apache/arrow-go/v18/arrow"
)

var (
	ColumnMetadataBuiltinMessage   = ColumnMetadata(ColumnTypeBuiltin, Loki.String)
	ColumnMetadataBuiltinTimestamp = ColumnMetadata(ColumnTypeBuiltin, Loki.Timestamp)
)

func ColumnMetadata(ct ColumnType, dt DataType) arrow.Metadata {
	return arrow.NewMetadata(
		[]string{MetadataKeyColumnType, MetadataKeyColumnDataType},
		[]string{ct.String(), dt.String()},
	)
}
