package labelfilter

import (
	"fmt"

	"github.com/dustin/go-humanize"
	"github.com/grafana/loki/pkg/logql/log"
)

type Bytes struct {
	Name  string
	Value uint64
	Type  FilterType
}

func NewBytes(t FilterType, name string, b uint64) *Bytes {
	return &Bytes{
		Name:  name,
		Type:  t,
		Value: b,
	}
}

func (d *Bytes) Process(line []byte, lbs log.Labels) ([]byte, bool) {
	if lbs.HasError() {
		// if there's an error only the string matchers can filter it out.
		return line, true
	}
	for k, v := range lbs {
		if k == d.Name {
			value, err := humanize.ParseBytes(v)
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
				if err != nil {
					lbs.SetError("LabelFilterError")
					return line, true
				}
			}
		}
	}
	// we have not found this label.
	return line, false
}

func (d *Bytes) String() string {
	return fmt.Sprintf("%s%s%d", d.Name, d.Type, d.Value)
}
