package datatype

import (
	"fmt"
	"strconv"
	"time"
)

type NullLiteral struct {
}

// String implements Literal.
func (n *NullLiteral) String() string {
	return "null"
}

// Type implements Literal.
func (n *NullLiteral) Type() DataType {
	return Null
}

// Value implements Literal.
func (n *NullLiteral) Value() any {
	return nil
}

type BoolLiteral struct {
	v bool
}

// String implements Literal.
func (b *BoolLiteral) String() string {
	return strconv.FormatBool(b.v)
}

// Type implements Literal.
func (b *BoolLiteral) Type() DataType {
	return Bool
}

// Value implements Literal.
func (b *BoolLiteral) Value() any {
	return b.v
}

type StringLiteral struct {
	v string
}

// String implements Literal.
func (s *StringLiteral) String() string {
	return fmt.Sprintf(`"%s"`, s.v)
}

// Type implements Literal.
func (s *StringLiteral) Type() DataType {
	return String
}

// Value implements Literal.
func (s *StringLiteral) Value() any {
	return s.v
}

type IntegerLiteral struct {
	v int64
}

// String implements Literal.
func (i *IntegerLiteral) String() string {
	return strconv.FormatInt(i.v, 10)
}

// Type implements Literal.
func (i *IntegerLiteral) Type() DataType {
	return Integer
}

// Value implements Literal.
func (i *IntegerLiteral) Value() any {
	return i.v
}

type FloatLiteral struct {
	v float64
}

// String implements Literal.
func (f *FloatLiteral) String() string {
	return strconv.FormatFloat(f.v, 'f', -1, 64)
}

// Type implements Literal.
func (f *FloatLiteral) Type() DataType {
	return Float
}

// Value implements Literal.
func (f *FloatLiteral) Value() any {
	return f.v
}

type TimestampLiteral struct {
	v int64
}

// String implements Literal.
func (t *TimestampLiteral) String() string {
	return time.Unix(0, t.v).UTC().Format(time.RFC3339Nano)
}

// Type implements Literal.
func (t *TimestampLiteral) Type() DataType {
	return Timestamp
}

// Value implements Literal.
func (t *TimestampLiteral) Value() any {
	return t.v
}

type DurationLiteral struct {
	v int64
}

// String implements Literal.
func (d *DurationLiteral) String() string {
	return fmt.Sprintf("%dns", d.v)
}

// Type implements Literal.
func (d *DurationLiteral) Type() DataType {
	return Duration
}

// Value implements Literal.
func (d *DurationLiteral) Value() any {
	return d.v
}

type BytesLiteral struct {
	v int64
}

// String implements Literal.
func (b *BytesLiteral) String() string {
	return fmt.Sprintf("%dB", b.v)
}

// Type implements Literal.
func (b *BytesLiteral) Type() DataType {
	return Bytes
}

// Value implements Literal.
func (b *BytesLiteral) Value() any {
	return b.v
}

// Literal is holds a value of [any] typed as [DataType].
type Literal interface {
	fmt.Stringer
	Value() any
	Type() DataType
}

var (
	_ Literal = (*NullLiteral)(nil)
	_ Literal = (*BoolLiteral)(nil)
	_ Literal = (*StringLiteral)(nil)
	_ Literal = (*IntegerLiteral)(nil)
	_ Literal = (*FloatLiteral)(nil)
	_ Literal = (*TimestampLiteral)(nil)
	_ Literal = (*DurationLiteral)(nil)
	_ Literal = (*BytesLiteral)(nil)
)

func NewNullLiteral() *NullLiteral {
	return &NullLiteral{}
}

func NewBoolLiteral(v bool) *BoolLiteral {
	return &BoolLiteral{v: v}
}

func NewStringLiteral(v string) *StringLiteral {
	return &StringLiteral{v: v}
}

func NewIntegerLiteral(v int64) *IntegerLiteral {
	return &IntegerLiteral{v: v}
}

func NewFloatLiteral(v float64) *FloatLiteral {
	return &FloatLiteral{v: v}
}

func NewTimestampLiteral(v int64) *TimestampLiteral {
	return &TimestampLiteral{v: v}
}

func NewDurationLiteral(v int64) *DurationLiteral {
	return &DurationLiteral{v: v}
}

func NewBytesLiteral(v int64) *BytesLiteral {
	return &BytesLiteral{v: v}
}
