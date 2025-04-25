package logical

import (
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A Literal represents a literal value known at plan time. Literal only
// implements [Value].
//
// The zero value of a Literal is a NULL value.
type Literal struct {
	datatype.Literal
}

var _ Value = (*Literal)(nil)

func NewLiteral(v any) *Literal {
	if v == nil {
		return &Literal{Literal: datatype.NewNullLiteral()}
	}

	switch casted := v.(type) {
	case bool:
		return &Literal{Literal: datatype.NewBoolLiteral(casted)}
	case string:
		// TODO(chaudum): Try parsing bytes/timestamp/duration
		return &Literal{Literal: datatype.NewStringLiteral(casted)}
	case int:
		return &Literal{Literal: datatype.NewIntegerLiteral(int64(casted))}
	case int64:
		return &Literal{Literal: datatype.NewIntegerLiteral(casted)}
	case float64:
		return &Literal{Literal: datatype.NewFloatLiteral(casted)}
	case time.Time:
		return &Literal{Literal: datatype.NewTimestampLiteral(casted.UnixNano())}
	case time.Duration:
		return &Literal{Literal: datatype.NewDurationLiteral(casted.Nanoseconds())}
	default:
		return &Literal{Literal: datatype.NewNullLiteral()}
	}
}

// Kind returns the kind of value represented by the literal.
func (l Literal) Kind() datatype.DataType {
	return l.Literal.Type()
}

// Name returns the string form of the literal.
func (l Literal) Name() string {
	return l.Literal.String()
}

// String returns a printable form of the literal, even if lit is not a
// [ValueTypeString].
func (l Literal) String() string {
	return l.Literal.String()
}

// Value returns lit's value as untyped interface{}.
func (l Literal) Value() any {
	return l.Literal.Value()
}

func (l *Literal) Schema() *schema.Schema {
	// TODO(rfratto): schema.Schema needs to be updated to be a more general
	// "type" instead.
	return nil
}

func (l *Literal) isValue() {}
