// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package datetime

import (
	"strings"
	"time"
)

const (
	timeOnlyJSON = `"` + time.TimeOnly + `"`
)

// PlainTime represents a time value without date information. It supports HH:MM:SS format
// with optional nanosecond precision and timezone information.
type PlainTime time.Time

// MarshalJSON marshals the PlainTime to a JSON byte slice.
func (p PlainTime) MarshalJSON() ([]byte, error) {
	return []byte(time.Time(p).Format(timeOnlyJSON)), nil
}

// MarshalText returns a textual representation of PlainTime.
func (p PlainTime) MarshalText() ([]byte, error) {
	return []byte(time.Time(p).Format(time.TimeOnly)), nil
}

// UnmarshalJSON unmarshals a JSON byte slice into PlainTime.
func (p *PlainTime) UnmarshalJSON(data []byte) error {
	return p.parse(timeOnlyJSON, string(data))
}

// UnmarshalText decodes the textual representation of PlainTime.
func (p *PlainTime) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		// empty XML element means no value
		return nil
	}
	return p.parse(time.TimeOnly, string(data))
}

// parse parses a time string using the specified layout
func (p *PlainTime) parse(layout, value string) error {
	t, err := time.Parse(layout, strings.ToUpper(value))
	*p = PlainTime(t)
	return err
}

// String returns the string of PlainTime.
func (p PlainTime) String() string {
	tt := time.Time(p)
	return tt.Format(time.TimeOnly)
}
