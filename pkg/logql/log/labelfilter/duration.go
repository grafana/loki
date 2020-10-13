package labelfilter

import (
	"fmt"
	"time"

	"github.com/grafana/loki/pkg/logql/log"
)

type Duration struct {
	Name  string
	Value time.Duration
	Type  FilterType
}

func NewDuration(t FilterType, name string, d time.Duration) *Duration {
	return &Duration{
		Name:  name,
		Type:  t,
		Value: d,
	}
}

func (d *Duration) Process(line []byte, lbs log.Labels) ([]byte, bool) {
	if lbs.HasError() {
		// if there's an error only the string matchers can filter out.
		return line, true
	}
	for k, v := range lbs {
		if k == d.Name {
			value, err := time.ParseDuration(v)
			if err != nil {
				lbs.SetError("LabelFilterError")
				return line, true
			}
			switch d.Type {
			case FilterEqual:
				return line, value == d.Value
			case FilterNotEqual:
				return line, value != d.Value
			case FilterGreaterThan:
				return line, value > d.Value
			case FilterGreaterThanOrEqual:
				return line, value >= d.Value
			case FilterLesserThan:
				return line, value < d.Value
			case FilterLesserThanOrEqual:
				return line, value <= d.Value
			default:
				lbs.SetError("LabelFilterError")
				return line, true
			}
		}
	}
	// we have not found this label.
	return line, false
}

func (d *Duration) String() string {
	return fmt.Sprintf("%s%s%s", d.Name, d.Type, d.Value)
}
