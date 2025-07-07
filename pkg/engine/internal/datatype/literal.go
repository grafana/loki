package datatype

import (
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/util"
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

// Any implements Literal.
func (n *NullLiteral) Any() any {
	return nil
}

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

// Any implements Literal.
func (b *BoolLiteral) Any() any {
	return b.v
}

func (b *BoolLiteral) Value() bool {
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

// Any implements Literal.
func (s *StringLiteral) Any() any {
	return s.v
}

func (s *StringLiteral) Value() string {
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

// Any implements Literal.
func (i *IntegerLiteral) Any() any {
	return i.v
}

func (i *IntegerLiteral) Value() int64 {
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

// Any implements Literal.
func (f *FloatLiteral) Any() any {
	return f.v
}

func (f *FloatLiteral) Value() float64 {
	return f.v
}

type TimestampLiteral struct {
	v int64 // unixnano, UTC
}

// String implements Literal.
func (t *TimestampLiteral) String() string {
	return util.FormatTimeRFC3339Nano(time.Unix(0, t.v))
}

// Type implements Literal.
func (t *TimestampLiteral) Type() DataType {
	return Timestamp
}

// Any implements Literal.
func (t *TimestampLiteral) Any() any {
	return t.Value()
}

func (t *TimestampLiteral) Value() time.Time {
	return time.Unix(0, t.v).UTC()
}

type DurationLiteral struct {
	v time.Duration
}

// String implements Literal.
func (d *DurationLiteral) String() string {
	return d.v.String()
}

// Type implements Literal.
func (d *DurationLiteral) Type() DataType {
	return Duration
}

// Any implements Literal.
func (d *DurationLiteral) Any() any {
	return d.v
}

func (d *DurationLiteral) Value() time.Duration {
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

// Any implements Literal.
func (b *BytesLiteral) Any() any {
	return b.v
}

func (b *BytesLiteral) Value() int64 {
	return b.v
}

// Literal is holds a value of [any] typed as [DataType].
type Literal interface {
	fmt.Stringer
	Any() any
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

func NewTimestampLiteral(v time.Time) *TimestampLiteral {
	return &TimestampLiteral{v: v.UTC().UnixNano()}
}

func NewDurationLiteral(v time.Duration) *DurationLiteral {
	return &DurationLiteral{v: v}
}

func NewBytesLiteral(v int64) *BytesLiteral {
	return &BytesLiteral{v: v}
}
