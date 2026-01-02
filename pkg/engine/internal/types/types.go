package types //nolint:revive

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

type Null any
type Timestamp int64
type Duration int64
type Bytes int64

type Type uint8

const (
	NULL      = Type(arrow.NULL)
	BOOL      = Type(arrow.BOOL)
	STRING    = Type(arrow.STRING)
	INT64     = Type(arrow.INT64)
	FLOAT64   = Type(arrow.FLOAT64)
	TIMESTAMP = Type(arrow.TIMESTAMP)
	STRUCT    = Type(arrow.STRUCT)
	LIST      = Type(arrow.LIST)
)

func (t Type) String() string {
	switch t {
	case NULL:
		return "NULL"
	case BOOL:
		return "BOOL"
	case STRING:
		return "STRING"
	case INT64:
		return "INT64"
	case FLOAT64:
		return "FLOAT64"
	case TIMESTAMP:
		return "TIMESTAMP"
	case STRUCT:
		return "STRUCT"
	case LIST:
		return "LIST"
	default:
		return "INVALID"
	}
}

type DataType interface {
	fmt.Stringer
	ID() Type
	ArrowType() arrow.DataType
}

type tNull struct{}

func (tNull) ID() Type                  { return NULL }
func (tNull) String() string            { return "null" }
func (tNull) ArrowType() arrow.DataType { return Arrow.Null }

type tBool struct{}

func (tBool) ID() Type                  { return BOOL }
func (tBool) String() string            { return "bool" }
func (tBool) ArrowType() arrow.DataType { return Arrow.Bool }

type tString struct{}

func (tString) ID() Type                  { return STRING }
func (tString) String() string            { return "utf8" }
func (tString) ArrowType() arrow.DataType { return Arrow.String }

type tInteger struct{}

func (tInteger) ID() Type                  { return INT64 }
func (tInteger) String() string            { return "int64" }
func (tInteger) ArrowType() arrow.DataType { return Arrow.Integer }

type tFloat struct{}

func (tFloat) ID() Type                  { return FLOAT64 }
func (tFloat) String() string            { return "float64" }
func (tFloat) ArrowType() arrow.DataType { return Arrow.Float }

type tTimestamp struct{}

func (tTimestamp) ID() Type                  { return TIMESTAMP }
func (tTimestamp) String() string            { return "timestamp_ns" }
func (tTimestamp) ArrowType() arrow.DataType { return Arrow.Timestamp }

type tDuration struct{}

func (tDuration) ID() Type                  { return INT64 }
func (tDuration) String() string            { return "duration_ns" }
func (tDuration) ArrowType() arrow.DataType { return Arrow.Duration }

type tBytes struct{}

func (tBytes) ID() Type                  { return INT64 }
func (tBytes) String() string            { return "bytes" }
func (tBytes) ArrowType() arrow.DataType { return Arrow.Bytes }

type tStruct struct {
	arrowType *arrow.StructType
}

func (t tStruct) ID() Type                  { return STRUCT }
func (t tStruct) String() string            { return "struct" }
func (t tStruct) ArrowType() arrow.DataType { return t.arrowType }

// NewStructType creates a DataType from an Arrow StructType
func NewStructType(arrowType *arrow.StructType) DataType {
	return tStruct{arrowType: arrowType}
}

type tList struct {
	arrowType *arrow.ListType
}

func (t tList) ID() Type                  { return LIST }
func (t tList) String() string            { return "list" }
func (t tList) ArrowType() arrow.DataType { return t.arrowType }

var (
	names = map[string]DataType{
		Loki.Null.String():      Loki.Null,
		Loki.Bool.String():      Loki.Bool,
		Loki.String.String():    Loki.String,
		Loki.Integer.String():   Loki.Integer,
		Loki.Float.String():     Loki.Float,
		Loki.Timestamp.String(): Loki.Timestamp,
		Loki.Duration.String():  Loki.Duration,
		Loki.Bytes.String():     Loki.Bytes,
		Loki.Struct.String():    Loki.Struct,
		Loki.List.String():      Loki.List,
	}
)

func FromString(dt string) (DataType, error) {
	ty, ok := names[dt]
	if !ok {
		return Loki.Null, fmt.Errorf("invalid data type: %s", dt)
	}
	return ty, nil
}

func MustFromString(dt string) DataType {
	ty, err := FromString(dt)
	if err != nil {
		panic(err)
	}
	return ty
}
