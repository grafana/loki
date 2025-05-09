package datatype

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

type Timestamp int64
type Duration int64
type Bytes int64

type Type uint8

const (
	NULL    = Type(arrow.NULL)
	BOOL    = Type(arrow.BOOL)
	STRING  = Type(arrow.STRING)
	INT64   = Type(arrow.INT64)
	FLOAT64 = Type(arrow.FLOAT64)
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
func (tNull) ArrowType() arrow.DataType { return ArrowType.Null }

type tBool struct{}

func (tBool) ID() Type                  { return BOOL }
func (tBool) String() string            { return "bool" }
func (tBool) ArrowType() arrow.DataType { return ArrowType.Bool }

type tString struct{}

func (tString) ID() Type                  { return STRING }
func (tString) String() string            { return "string" }
func (tString) ArrowType() arrow.DataType { return ArrowType.String }

type tInteger struct{}

func (tInteger) ID() Type                  { return INT64 }
func (tInteger) String() string            { return "integer" }
func (tInteger) ArrowType() arrow.DataType { return ArrowType.Integer }

type tFloat struct{}

func (tFloat) ID() Type                  { return FLOAT64 }
func (tFloat) String() string            { return "float" }
func (tFloat) ArrowType() arrow.DataType { return ArrowType.Float }

type tTimestamp struct{}

func (tTimestamp) ID() Type                  { return INT64 }
func (tTimestamp) String() string            { return "timestamp" }
func (tTimestamp) ArrowType() arrow.DataType { return ArrowType.Integer }

type tDuration struct{}

func (tDuration) ID() Type                  { return INT64 }
func (tDuration) String() string            { return "duration" }
func (tDuration) ArrowType() arrow.DataType { return ArrowType.Integer }

type tBytes struct{}

func (tBytes) ID() Type                  { return INT64 }
func (tBytes) String() string            { return "bytes" }
func (tBytes) ArrowType() arrow.DataType { return ArrowType.Integer }

var (
	names = map[string]DataType{
		LokiType.Null.String():      LokiType.Null,
		LokiType.Bool.String():      LokiType.Bool,
		LokiType.String.String():    LokiType.String,
		LokiType.Integer.String():   LokiType.Integer,
		LokiType.Float.String():     LokiType.Float,
		LokiType.Timestamp.String(): LokiType.Timestamp,
		LokiType.Duration.String():  LokiType.Duration,
		LokiType.Bytes.String():     LokiType.Bytes,
	}
)

func FromString(dt string) DataType {
	ty, ok := names[dt]
	if !ok {
		panic("invalid data type name")
	}
	return ty
}
