package labelfilter

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	Noop = noopFilter{}
)

type Filterer interface {
	Filter(lbs labels.Labels) (bool, error)
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

func (b *Binary) Filter(lbs labels.Labels) (bool, error) {
	l, err := b.Left.Filter(lbs)
	if err != nil {
		return false, err
	}
	if !b.and && l {
		return true, nil
	}
	r, err := b.Right.Filter(lbs)
	if err != nil {
		return false, err
	}
	if !b.and {
		return l || r, nil
	}
	return l && r, nil
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

func (noopFilter) Filter(lbs labels.Labels) (bool, error) { return true, nil }

func (noopFilter) String() string { return "" }

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
