package labelfilter

import (
	"fmt"
	"strconv"

	"github.com/prometheus/prometheus/pkg/labels"
)

type Numeric struct {
	Name  string
	Value float64
	Type  FilterType
}

func NewNumeric(t FilterType, name string, v float64) *Numeric {
	return &Numeric{
		Name:  name,
		Type:  t,
		Value: v,
	}
}

func (n *Numeric) Filter(lbs labels.Labels) (bool, error) {
	for _, l := range lbs {
		if l.Name == n.Name {
			value, err := strconv.ParseFloat(l.Value, 64)
			if err != nil {
				return false, errConversion
			}
			switch n.Type {
			case FilterEqual:
				return value == n.Value, nil
			case FilterNotEqual:
				return value != n.Value, nil
			case FilterGreaterThan:
				return value > n.Value, nil
			case FilterGreaterThanOrEqual:
				return value >= n.Value, nil
			case FilterLesserThan:
				return value < n.Value, nil
			case FilterLesserThanOrEqual:
				return value <= n.Value, nil
			default:
				return false, errUnsupportedType
			}
		}
	}
	return false, nil
}

func (n *Numeric) String() string {
	return fmt.Sprintf("%s%s%s", n.Name, n.Type, strconv.FormatFloat(n.Value, 'f', -1, 64))
}
