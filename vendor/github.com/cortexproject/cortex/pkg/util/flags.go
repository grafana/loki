package util

import (
	"flag"
	"net/url"
	"time"

	"github.com/prometheus/common/model"
)

const secondsInDay = 24 * 60 * 60

// Registerer is a thing that can RegisterFlags
type Registerer interface {
	RegisterFlags(*flag.FlagSet)
}

// RegisterFlags registers flags with the provided Registerers
func RegisterFlags(rs ...Registerer) {
	for _, r := range rs {
		r.RegisterFlags(flag.CommandLine)
	}
}

// DefaultValues intiates a set of configs (Registerers) with their defaults.
func DefaultValues(rs ...Registerer) {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	for _, r := range rs {
		r.RegisterFlags(fs)
	}
	fs.Parse([]string{})
}

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

// URLValue is a url.URL that can be used as a flag.
type URLValue struct {
	*url.URL
}

// String implements flag.Value
func (v URLValue) String() string {
	if v.URL == nil {
		return ""
	}
	return v.URL.String()
}

// Set implements flag.Value
func (v *URLValue) Set(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	v.URL = u
	return nil
}
