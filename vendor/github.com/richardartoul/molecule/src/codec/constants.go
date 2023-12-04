// This file contains modifications from the original source code found in: https://github.com/jhump/protoreflect

// Package codec contains all the logic required for interacting with the protobuf raw encoding. It also contains
// all the encoding and field type specific constants required for using the molecule library.
package codec

// WireType represents a protobuf encoding wire type.
type WireType int8

// Constants that identify the encoding of a value on the wire.
const (
	WireVarint     WireType = 0
	WireFixed64    WireType = 1
	WireBytes      WireType = 2
	WireStartGroup WireType = 3
	WireEndGroup   WireType = 4
	WireFixed32    WireType = 5
)

// FieldType represents a protobuf field type.
type FieldType int32

const (
	FieldType_DOUBLE   FieldType = 1
	FieldType_FLOAT    FieldType = 2
	FieldType_INT64    FieldType = 3
	FieldType_UINT64   FieldType = 4
	FieldType_INT32    FieldType = 5
	FieldType_FIXED64  FieldType = 6
	FieldType_FIXED32  FieldType = 7
	FieldType_BOOL     FieldType = 8
	FieldType_STRING   FieldType = 9
	FieldType_GROUP    FieldType = 10
	FieldType_MESSAGE  FieldType = 11
	FieldType_BYTES    FieldType = 12
	FieldType_UINT32   FieldType = 13
	FieldType_ENUM     FieldType = 14
	FieldType_SFIXED32 FieldType = 15
	FieldType_SFIXED64 FieldType = 16
	FieldType_SINT32   FieldType = 17
	FieldType_SINT64   FieldType = 18
)
