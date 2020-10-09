package labelfilter

import (
	"fmt"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/prometheus/pkg/labels"
)

type Bytes struct {
	Name  string
	Value uint64 
	Type  FilterType
}

func NewBytes(t FilterType, name string, b uint64) *Bytes{
	return &Bytes{
		Name:  name,
		Type:  t,
		Value: b,
	}
}

func (d *Bytes) Filter(lbs labels.Labels) (bool, error) {
	for _, l := range lbs {
		if l.Name == d.Name {
			value, err := humanize.ParseBytes(l.Value)
			if err != nil {
				return false, errConversion
			}
			switch d.Type {
			case FilterEqual:
				return value == d.Value, nil
			case FilterNotEqual:
				return value != d.Value, nil
			case FilterGreaterThan:
				return value > d.Value, nil
			case FilterGreaterThanOrEqual:
				return value >= d.Value, nil
			case FilterLesserThan:
				return value < d.Value, nil
			case FilterLesserThanOrEqual:
				return value <= d.Value, nil
			default:
				return false, errUnsupportedType
			}
		}
	}
	// we have not found this label.
	return false, nil
}

func (d *Bytes) String() string {
	return fmt.Sprintf("%s%s%d", d.Name, d.Type, d.Value)
}