package labelfilter

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
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

var (
	errConversion      = errors.New("converting label value failed")
	errUnsupportedType = errors.New("unsupported filter type")
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

func (d *Duration) Filter(lbs labels.Labels) (bool, error) {
	for _, l := range lbs {
		if l.Name == d.Name {
			value, err := time.ParseDuration(l.Value)
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

func (d *Duration) String() string {
	return fmt.Sprintf("%s%s%s", d.Name, d.Type, d.Value)
}
