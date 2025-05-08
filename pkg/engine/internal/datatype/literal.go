package datatype

import (
	"fmt"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"
)

type NullLiteral struct {
}

// String implements Literal.
func (n NullLiteral) String() string {
	return "null"
}

// Type implements Literal.
func (n NullLiteral) Type() DataType {
	return Loki.Null
}

// Any implements Literal.
func (n NullLiteral) Any() any {
	return nil
}

func (n NullLiteral) Value() any {
	return nil
}

type BoolLiteral bool

// String implements Literal.
func (b BoolLiteral) String() string {
	return strconv.FormatBool(bool(b))
}

// Type implements Literal.
func (b BoolLiteral) Type() DataType {
	return Loki.Bool
}

// Any implements Literal.
func (b BoolLiteral) Any() any {
	return b.Value()
}

func (b BoolLiteral) Value() bool {
	return bool(b)
}

type StringLiteral string

// String implements Literal.
func (s StringLiteral) String() string {
	return fmt.Sprintf(`"%v"`, string(s))
}

// Type implements Literal.
func (s StringLiteral) Type() DataType {
	return Loki.String
}

// Any implements Literal.
func (s StringLiteral) Any() any {
	return s.Value()
}

func (s StringLiteral) Value() string {
	return string(s)
}

type IntegerLiteral int64

// String implements Literal.
func (i IntegerLiteral) String() string {
	return strconv.FormatInt(int64(i), 10)
}

// Type implements Literal.
func (i IntegerLiteral) Type() DataType {
	return Loki.Integer
}

// Any implements Literal.
func (i IntegerLiteral) Any() any {
	return i.Value()
}

func (i IntegerLiteral) Value() int64 {
	return int64(i)
}

type FloatLiteral float64

// String implements Literal.
func (f FloatLiteral) String() string {
	return strconv.FormatFloat(float64(f), 'f', -1, 64)
}

// Type implements Literal.
func (f FloatLiteral) Type() DataType {
	return Loki.Float
}

// Any implements Literal.
func (f FloatLiteral) Any() any {
	return f.Value()
}

func (f FloatLiteral) Value() float64 {
	return float64(f)
}

type TimestampLiteral Timestamp

// String implements Literal.
func (t TimestampLiteral) String() string {
	return time.Unix(0, int64(t.Value())).UTC().Format(time.RFC3339Nano)
}

// Type implements Literal.
func (t TimestampLiteral) Type() DataType {
	return Loki.Timestamp
}

// Any implements Literal.
func (t TimestampLiteral) Any() any {
	return t.Value()
}

func (t TimestampLiteral) Value() Timestamp {
	return Timestamp(t)
}

type DurationLiteral int64

// String implements Literal.
func (d DurationLiteral) String() string {
	return time.Duration(d).String()
}

// Type implements Literal.
func (d DurationLiteral) Type() DataType {
	return Loki.Duration
}

// Any implements Literal.
func (d DurationLiteral) Any() any {
	return d.Value()
}

func (d DurationLiteral) Value() Duration {
	return Duration(d)
}

type BytesLiteral Bytes

// String implements Literal.
func (b BytesLiteral) String() string {
	return humanize.IBytes(uint64(b))
}

// Type implements Literal.
func (b BytesLiteral) Type() DataType {
	return Loki.Bytes
}

// Any implements Literal.
func (b BytesLiteral) Any() any {
	return b.Value()
}

func (b BytesLiteral) Value() Bytes {
	return Bytes(b)
}

// Literal is holds a value of [any] typed as [DataType].
type Literal interface {
	fmt.Stringer
	Any() any
	Type() DataType
}

type LiteralType interface {
	any | bool | string | int64 | float64 | Timestamp | Duration | Bytes
}

type TypedLiteral[T LiteralType] interface {
	Literal
	Value() T
}

var (
	_ Literal                 = (*NullLiteral)(nil)
	_ TypedLiteral[any]       = (*NullLiteral)(nil)
	_ Literal                 = (*BoolLiteral)(nil)
	_ TypedLiteral[bool]      = (*BoolLiteral)(nil)
	_ Literal                 = (*StringLiteral)(nil)
	_ TypedLiteral[string]    = (*StringLiteral)(nil)
	_ Literal                 = (*IntegerLiteral)(nil)
	_ TypedLiteral[int64]     = (*IntegerLiteral)(nil)
	_ Literal                 = (*FloatLiteral)(nil)
	_ TypedLiteral[float64]   = (*FloatLiteral)(nil)
	_ Literal                 = (*TimestampLiteral)(nil)
	_ TypedLiteral[Timestamp] = (*TimestampLiteral)(nil)
	_ Literal                 = (*DurationLiteral)(nil)
	_ TypedLiteral[Duration]  = (*DurationLiteral)(nil)
	_ Literal                 = (*BytesLiteral)(nil)
	_ TypedLiteral[Bytes]     = (*BytesLiteral)(nil)
)

func NewLiteral[T LiteralType](value T) Literal {
	switch val := any(value).(type) {
	case bool:
		return BoolLiteral(val)
	case string:
		return StringLiteral(val)
	case int64:
		return IntegerLiteral(val)
	case float64:
		return FloatLiteral(val)
	case Timestamp:
		return TimestampLiteral(val)
	case Duration:
		return DurationLiteral(val)
	case Bytes:
		return BytesLiteral(val)
	}
	panic(fmt.Sprintf("invalid literal value type %T", value))
}

func NewNullLiteral() NullLiteral {
	return NullLiteral{}
}
