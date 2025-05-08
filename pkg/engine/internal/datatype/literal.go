package datatype

import (
	"fmt"
	"strconv"
	"time"
)

type NullLiteral struct {
}

// String implements Literal.
func (n NullLiteral) String() string {
	return "null"
}

// Type implements Literal.
func (n NullLiteral) Type() DataType {
	return Null
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
	return Bool
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
	return String
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
	return Integer
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
	return Float
}

// Any implements Literal.
func (f FloatLiteral) Any() any {
	return f.Value()
}

func (f FloatLiteral) Value() float64 {
	return float64(f)
}

type TimestampLiteral int64

// String implements Literal.
func (t TimestampLiteral) String() string {
	return time.Unix(0, t.Value()).UTC().Format(time.RFC3339Nano)
}

// Type implements Literal.
func (t TimestampLiteral) Type() DataType {
	return Timestamp
}

// Any implements Literal.
func (t TimestampLiteral) Any() any {
	return t.Value()
}

func (t TimestampLiteral) Value() int64 {
	return int64(t)
}

type DurationLiteral int64

// String implements Literal.
func (d DurationLiteral) String() string {
	return time.Duration(d).String()
}

// Type implements Literal.
func (d DurationLiteral) Type() DataType {
	return Duration
}

// Any implements Literal.
func (d DurationLiteral) Any() any {
	return d.Value()
}

func (d DurationLiteral) Value() int64 {
	return int64(d)
}

type BytesLiteral int64

// String implements Literal.
func (b BytesLiteral) String() string {
	// return humanize.Bytes(uint64(b))
	return fmt.Sprintf("%dB", b)
}

// Type implements Literal.
func (b BytesLiteral) Type() DataType {
	return Bytes
}

// Any implements Literal.
func (b BytesLiteral) Any() any {
	return b.Value()
}

func (b BytesLiteral) Value() int64 {
	return int64(b)
}

// Literal is holds a value of [any] typed as [DataType].
type Literal interface {
	fmt.Stringer
	Any() any
	Type() DataType
}

type LiteralType interface {
	any | bool | string | int64 | float64 | time.Time | time.Duration
}

type TypedLiteral[T LiteralType] interface {
	Literal
	Value() T
}

var (
	_ Literal               = (*NullLiteral)(nil)
	_ TypedLiteral[any]     = (*NullLiteral)(nil)
	_ Literal               = (*BoolLiteral)(nil)
	_ TypedLiteral[bool]    = (*BoolLiteral)(nil)
	_ Literal               = (*StringLiteral)(nil)
	_ TypedLiteral[string]  = (*StringLiteral)(nil)
	_ Literal               = (*IntegerLiteral)(nil)
	_ TypedLiteral[int64]   = (*IntegerLiteral)(nil)
	_ Literal               = (*FloatLiteral)(nil)
	_ TypedLiteral[float64] = (*FloatLiteral)(nil)
	_ Literal               = (*TimestampLiteral)(nil)
	_ TypedLiteral[int64]   = (*TimestampLiteral)(nil)
	_ Literal               = (*DurationLiteral)(nil)
	_ TypedLiteral[int64]   = (*DurationLiteral)(nil)
	_ Literal               = (*BytesLiteral)(nil)
	_ TypedLiteral[int64]   = (*BytesLiteral)(nil)
)

func NewNullLiteral() NullLiteral {
	return NullLiteral{}
}

func NewBoolLiteral(v bool) BoolLiteral {
	return BoolLiteral(v)
}

func NewStringLiteral(v string) StringLiteral {
	return StringLiteral(v)
}

func NewIntegerLiteral(v int64) IntegerLiteral {
	return IntegerLiteral(v)
}

func NewFloatLiteral(v float64) FloatLiteral {
	return FloatLiteral(v)
}

func NewTimestampLiteral(v time.Time) TimestampLiteral {
	return TimestampLiteral(v.UTC().UnixNano())
}

func NewDurationLiteral(v time.Duration) DurationLiteral {
	return DurationLiteral(v.Nanoseconds())
}

func NewBytesLiteral(v int64) BytesLiteral {
	return BytesLiteral(v)
}
