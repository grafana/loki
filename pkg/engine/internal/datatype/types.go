package datatype

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

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
}

var (
	Null      DataType = tNull{}
	Bool      DataType = tBool{}
	String    DataType = tString{}
	Integer   DataType = tInteger{}
	Float     DataType = tFloat{}
	Timestamp DataType = tTimestamp{}
	Duration  DataType = tDuration{}
	Bytes     DataType = tBytes{}
)

type tNull struct{}

func (tNull) ID() Type       { return NULL }
func (tNull) String() string { return "null" }

type tBool struct{}

func (tBool) ID() Type       { return BOOL }
func (tBool) String() string { return "bool" }

type tString struct{}

func (tString) ID() Type       { return STRING }
func (tString) String() string { return "string" }

type tInteger struct{}

func (tInteger) ID() Type       { return INT64 }
func (tInteger) String() string { return "integer" }

type tFloat struct{}

func (tFloat) ID() Type       { return FLOAT64 }
func (tFloat) String() string { return "float" }

type tTimestamp struct{}

func (tTimestamp) ID() Type       { return INT64 }
func (tTimestamp) String() string { return "timestamp" }

type tDuration struct{}

func (tDuration) ID() Type       { return INT64 }
func (tDuration) String() string { return "duration" }

type tBytes struct{}

func (tBytes) ID() Type       { return INT64 }
func (tBytes) String() string { return "bytes" }

var (
	names = map[string]DataType{
		Null.String():      Null,
		Bool.String():      Bool,
		String.String():    String,
		Integer.String():   Integer,
		Float.String():     Float,
		Timestamp.String(): Timestamp,
		Duration.String():  Duration,
		Bytes.String():     Bytes,
	}
)

func FromString(dt string) DataType {
	ty, ok := names[dt]
	if !ok {
		panic("invalid data type name")
	}
	return ty
}
