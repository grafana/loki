package labelfilter

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/pkg/logql/log"
)

var (
	_ Filterer = &Binary{}
	_ Filterer = &Bytes{}
	_ Filterer = &Duration{}
	_ Filterer = &String{}
	_ Filterer = &Numeric{}

	Noop = noopFilter{}
)

// FilterType is an enum for label filtering types.
type FilterType int

func (f FilterType) String() string {
	switch f {
	case FilterEqual:
		return "=="
	case FilterNotEqual:
		return "!="
	case FilterGreaterThan:
		return ">"
	case FilterGreaterThanOrEqual:
		return ">="
	case FilterLesserThan:
		return "<"
	case FilterLesserThanOrEqual:
		return "<="
	default:
		return ""
	}
}

// Possible FilterTypes.
const (
	FilterEqual FilterType = iota
	FilterNotEqual
	FilterGreaterThan
	FilterGreaterThanOrEqual
	FilterLesserThan
	FilterLesserThanOrEqual
)

type Filterer interface {
	log.Stage
	fmt.Stringer
}

type Binary struct {
	Left  Filterer
	Right Filterer
	and   bool
}

func NewAnd(left Filterer, right Filterer) *Binary {
	return &Binary{
		Left:  left,
		Right: right,
		and:   true,
	}
}

func NewOr(left Filterer, right Filterer) *Binary {
	return &Binary{
		Left:  left,
		Right: right,
	}
}

func (b *Binary) Process(line []byte, lbs log.Labels) ([]byte, bool) {
	line, lok := b.Left.Process(line, lbs)
	if !b.and && lok {
		return line, true
	}
	line, rok := b.Right.Process(line, lbs)
	if !b.and {
		return line, lok || rok
	}
	return line, lok && rok
}

func (b *Binary) String() string {
	var sb strings.Builder
	sb.WriteString("( ")
	sb.WriteString(b.Left.String())
	if b.and {
		sb.WriteString(" , ")
	} else {
		sb.WriteString(" or ")
	}
	sb.WriteString(b.Right.String())
	sb.WriteString(" )")
	return sb.String()
}

type noopFilter struct{}

func (noopFilter) String() string                                     { return "" }
func (noopFilter) Process(line []byte, lbs log.Labels) ([]byte, bool) { return line, true }

func ReduceAnd(filters []Filterer) Filterer {
	if len(filters) == 0 {
		return Noop
	}
	result := filters[0]
	for _, f := range filters[0:] {
		result = NewAnd(result, f)
	}
	return result
}
