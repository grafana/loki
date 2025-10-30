package types //nolint:revive

import (
	"fmt"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
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
	return util.FormatTimeRFC3339Nano(time.Unix(0, int64(t)))
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

func NewLiteralFromKind(value physical.LiteralExpression) Literal {
	switch kind := value.Kind.(type) {
	case *physical.LiteralExpression_BoolLiteral:
		return BoolLiteral(kind.BoolLiteral.Value)
	case *physical.LiteralExpression_StringLiteral:
		return StringLiteral(kind.StringLiteral.Value)
	case *physical.LiteralExpression_IntegerLiteral:
		return IntegerLiteral(kind.IntegerLiteral.Value)
	case *physical.LiteralExpression_FloatLiteral:
		return FloatLiteral(kind.FloatLiteral.Value)
	case *physical.LiteralExpression_TimestampLiteral:
		return TimestampLiteral(kind.TimestampLiteral.Value)
	case *physical.LiteralExpression_DurationLiteral:
		return DurationLiteral(kind.DurationLiteral.Value)
	case *physical.LiteralExpression_BytesLiteral:
		return BytesLiteral(kind.BytesLiteral.Value)
	}
	panic(fmt.Sprintf("invalid literal value type %T", value.Kind))
}

func NewLiteral[T LiteralType](value T) Literal {
	switch val := any(value).(type) {
	case *physical.LiteralExpression_BoolLiteral:
		return BoolLiteral(val.BoolLiteral.Value)
	case bool:
		return BoolLiteral(val)
	case *physical.LiteralExpression_StringLiteral:
		return StringLiteral(val.StringLiteral.Value)
	case string:
		return StringLiteral(val)
	case *physical.LiteralExpression_IntegerLiteral:
		return IntegerLiteral(val.IntegerLiteral.Value)
	case int64:
		return IntegerLiteral(val)
	case *physical.LiteralExpression_FloatLiteral:
		return FloatLiteral(val.FloatLiteral.Value)
	case float64:
		return FloatLiteral(val)
	case *physical.LiteralExpression_TimestampLiteral:
		return TimestampLiteral(val.TimestampLiteral.Value)
	case Timestamp:
		return TimestampLiteral(val)
	case *physical.LiteralExpression_DurationLiteral:
		return DurationLiteral(val.DurationLiteral.Value)
	case Duration:
		return DurationLiteral(val)
	case *physical.LiteralExpression_BytesLiteral:
		return BytesLiteral(val.BytesLiteral.Value)
	case Bytes:
		return BytesLiteral(val)
	case *physical.LiteralExpression_NullLiteral:
		return NewNullLiteral()
	case nil:
		return NewNullLiteral()
	}
	panic(fmt.Sprintf("invalid literal value type %T", value))
}

func NewNullLiteral() NullLiteral {
	return NullLiteral{}
}
