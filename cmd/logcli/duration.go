package main

import (
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/model"
)

const sinceHelp = "Lookback window. Supported units: y, w, d, h, m, s, ms."

// durationValue implements kingpin.Value and kingpin.Getter using Prometheus model.Duration,
// supporting Prometheus time unit strings such as "y", "w", "d", "h", "m", "s", and "ms".
type durationValue struct {
	target *time.Duration
}

func newDurationValue(target *time.Duration) *durationValue {
	return &durationValue{target: target}
}

func (d *durationValue) Set(s string) error {
	v, err := model.ParseDuration(s)
	if err != nil {
		return err
	}
	*d.target = time.Duration(v)
	return nil
}

func (d *durationValue) Get() interface{} {
	if d.target == nil {
		return time.Duration(0)
	}
	return *d.target
}

func (d *durationValue) String() string {
	if d.target == nil {
		return ""
	}
	return model.Duration(*d.target).String()
}

func durationVar(s kingpin.Settings, target *time.Duration) {
	s.SetValue(newDurationValue(target))
}
