// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package model

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"gopkg.in/alecthomas/kingpin.v2"
)

// TimeOrDurationValue is a custom kingping parser for time in RFC3339
// or duration in Go's duration format, such as "300ms", "-1.5h" or "2h45m".
// Only one will be set.
type TimeOrDurationValue struct {
	Time *time.Time
	Dur  *model.Duration
}

// Set converts string to TimeOrDurationValue.
func (tdv *TimeOrDurationValue) Set(s string) error {
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		tdv.Time = &t
		return nil
	}

	// error parsing time, let's try duration.
	var minus bool
	if s[0] == '-' {
		minus = true
		s = s[1:]
	}
	dur, err := model.ParseDuration(s)
	if err != nil {
		return err
	}

	if minus {
		dur = dur * -1
	}
	tdv.Dur = &dur
	return nil
}

// String returns either time or duration.
func (tdv *TimeOrDurationValue) String() string {
	switch {
	case tdv.Time != nil:
		return tdv.Time.String()
	case tdv.Dur != nil:
		return tdv.Dur.String()
	}

	return "nil"
}

// PrometheusTimestamp returns TimeOrDurationValue converted to PrometheusTimestamp
// if duration is set now+duration is converted to Timestamp.
func (tdv *TimeOrDurationValue) PrometheusTimestamp() int64 {
	switch {
	case tdv.Time != nil:
		return timestamp.FromTime(*tdv.Time)
	case tdv.Dur != nil:
		return timestamp.FromTime(time.Now().Add(time.Duration(*tdv.Dur)))
	}

	return 0
}

// TimeOrDuration helper for parsing TimeOrDuration with kingpin.
func TimeOrDuration(flags *kingpin.FlagClause) *TimeOrDurationValue {
	value := new(TimeOrDurationValue)
	flags.SetValue(value)
	return value
}
