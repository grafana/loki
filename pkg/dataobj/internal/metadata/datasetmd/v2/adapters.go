package datasetmd

import (
	v1 "github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// All of the conversion functions here are temporary and will be removed once
// the v1 metadata is replaced by v2 metadata (which will happen in a future
// commit within this same PR).

func ToV1ValueType(ty PhysicalType) v1.ValueType {
	switch ty {
	case PHYSICAL_TYPE_UNSPECIFIED:
		return v1.VALUE_TYPE_UNSPECIFIED
	case PHYSICAL_TYPE_INT64:
		return v1.VALUE_TYPE_INT64
	case PHYSICAL_TYPE_UINT64:
		return v1.VALUE_TYPE_UINT64
	case PHYSICAL_TYPE_BINARY:
		return v1.VALUE_TYPE_BYTE_ARRAY
	}

	panic("not reachable")
}

func ToV2PhysicalType(ty v1.ValueType) PhysicalType {
	switch ty {
	case v1.VALUE_TYPE_UNSPECIFIED:
		return PHYSICAL_TYPE_UNSPECIFIED
	case v1.VALUE_TYPE_INT64:
		return PHYSICAL_TYPE_INT64
	case v1.VALUE_TYPE_UINT64:
		return PHYSICAL_TYPE_UINT64
	case v1.VALUE_TYPE_BYTE_ARRAY:
		return PHYSICAL_TYPE_BINARY
	}

	panic("not reachable")
}

func ToV1CompressionType(ty CompressionType) v1.CompressionType {
	switch ty {
	case COMPRESSION_TYPE_UNSPECIFIED:
		return v1.COMPRESSION_TYPE_UNSPECIFIED
	case COMPRESSION_TYPE_NONE:
		return v1.COMPRESSION_TYPE_NONE
	case COMPRESSION_TYPE_SNAPPY:
		return v1.COMPRESSION_TYPE_SNAPPY
	case COMPRESSION_TYPE_ZSTD:
		return v1.COMPRESSION_TYPE_ZSTD
	}

	panic("not reachable")
}

func ToV2CompressionType(ty v1.CompressionType) CompressionType {
	switch ty {
	case v1.COMPRESSION_TYPE_UNSPECIFIED:
		return COMPRESSION_TYPE_UNSPECIFIED
	case v1.COMPRESSION_TYPE_NONE:
		return COMPRESSION_TYPE_NONE
	case v1.COMPRESSION_TYPE_SNAPPY:
		return COMPRESSION_TYPE_SNAPPY
	case v1.COMPRESSION_TYPE_ZSTD:
		return COMPRESSION_TYPE_ZSTD
	}

	panic("not reachable")
}

func ToV1Statistics(stats *Statistics) *v1.Statistics {
	if stats == nil {
		return nil
	}

	return &v1.Statistics{
		MinValue:         stats.MinValue,
		MaxValue:         stats.MaxValue,
		CardinalityCount: stats.CardinalityCount,
	}
}

func ToV2Statistics(stats *v1.Statistics) *Statistics {
	if stats == nil {
		return nil
	}

	return &Statistics{
		MinValue:         stats.MinValue,
		MaxValue:         stats.MaxValue,
		CardinalityCount: stats.CardinalityCount,
	}
}

func ToV1Encoding(ty EncodingType) v1.EncodingType {
	switch ty {
	case ENCODING_TYPE_UNSPECIFIED:
		return v1.ENCODING_TYPE_UNSPECIFIED
	case ENCODING_TYPE_PLAIN:
		return v1.ENCODING_TYPE_PLAIN
	case ENCODING_TYPE_DELTA:
		return v1.ENCODING_TYPE_DELTA
	case ENCODING_TYPE_BITMAP:
		return v1.ENCODING_TYPE_BITMAP
	}

	panic("not reachable")
}

func ToV2Encoding(ty v1.EncodingType) EncodingType {
	switch ty {
	case v1.ENCODING_TYPE_UNSPECIFIED:
		return ENCODING_TYPE_UNSPECIFIED
	case v1.ENCODING_TYPE_PLAIN:
		return ENCODING_TYPE_PLAIN
	case v1.ENCODING_TYPE_DELTA:
		return ENCODING_TYPE_DELTA
	case v1.ENCODING_TYPE_BITMAP:
		return ENCODING_TYPE_BITMAP
	}

	panic("not reachable")
}
