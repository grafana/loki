package labelfilter

import (
	"fmt"
	"strconv"

	"github.com/grafana/loki/pkg/logql/log"
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

func (n *Numeric) Process(line []byte, lbs log.Labels) ([]byte, bool) {
	if lbs.HasError() {
		// if there's an error only the string matchers can filter out.
		return line, true
	}
	for k, v := range lbs {
		if k == n.Name {
			value, err := strconv.ParseFloat(v, 64)
			if err != nil {
				lbs.SetError("LabelFilterError")
				return line, true
			}
			switch n.Type {
			case FilterEqual:
				return line, value == n.Value
			case FilterNotEqual:
				return line, value != n.Value
			case FilterGreaterThan:
				return line, value > n.Value
			case FilterGreaterThanOrEqual:
				return line, value >= n.Value
			case FilterLesserThan:
				return line, value < n.Value
			case FilterLesserThanOrEqual:
				return line, value <= n.Value
			default:
				lbs.SetError("LabelFilterError")
				return line, true
			}
		}
	}
	return line, false
}

func (n *Numeric) String() string {
	return fmt.Sprintf("%s%s%s", n.Name, n.Type, strconv.FormatFloat(n.Value, 'f', -1, 64))
}
