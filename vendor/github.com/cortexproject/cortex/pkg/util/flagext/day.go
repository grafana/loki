package flagext

import (
	"time"

	"github.com/prometheus/common/model"
)

const secondsInDay = 24 * 60 * 60

// DayValue is a model.Time that can be used as a flag.
// NB it only parses days!
type DayValue struct {
	model.Time
	set bool
}

// NewDayValue makes a new DayValue; will round t down to the nearest midnight.
func NewDayValue(t model.Time) DayValue {
	return DayValue{
		Time: model.TimeFromUnix((t.Unix() / secondsInDay) * secondsInDay),
		set:  true,
	}
}

// String implements flag.Value
func (v DayValue) String() string {
	return v.Time.Time().Format(time.RFC3339)
}

// Set implements flag.Value
func (v *DayValue) Set(s string) error {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	v.Time = model.TimeFromUnix(t.Unix())
	v.set = true
	return nil
}

// IsSet returns true is the DayValue has been set.
func (v *DayValue) IsSet() bool {
	return v.set
}
