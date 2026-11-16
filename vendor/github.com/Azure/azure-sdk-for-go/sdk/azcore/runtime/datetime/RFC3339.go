// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package datetime

import (
	"regexp"
	"strings"
	"time"
)

// Azure reports time in UTC but it doesn't include the 'Z' time zone suffix in some cases.
var tzOffsetRegex = regexp.MustCompile(`(?:Z|z|\+|-)(?:\d+:\d+)*"*$`)

const (
	utcDateTime        = "2006-01-02T15:04:05.999999999"
	utcDateTimeJSON    = `"` + utcDateTime + `"`
	utcDateTimeNoT     = "2006-01-02 15:04:05.999999999"
	utcDateTimeJSONNoT = `"` + utcDateTimeNoT + `"`
	dateTimeNoT        = `2006-01-02 15:04:05.999999999Z07:00`
	dateTimeJSON       = `"` + time.RFC3339Nano + `"`
	dateTimeJSONNoT    = `"` + dateTimeNoT + `"`
)

// RFC3339 represents a date and time value in RFC 3339 format with nanosecond precision
// as defined in https://tools.ietf.org/html/rfc3339.
type RFC3339 time.Time

// MarshalJSON marshals the RFC3339 timestamp to a JSON byte slice.
func (r RFC3339) MarshalJSON() ([]byte, error) {
	return time.Time(r).MarshalJSON()
}

// MarshalText returns a textual representation of the RFC3339.
func (r RFC3339) MarshalText() ([]byte, error) {
	return time.Time(r).MarshalText()
}

// UnmarshalJSON unmarshals a JSON byte slice into an RFC3339 time.
func (r *RFC3339) UnmarshalJSON(data []byte) error {
	tzOffset := tzOffsetRegex.Match(data)
	hasT := strings.Contains(string(data), "T") || strings.Contains(string(data), "t")
	var layout string
	if tzOffset && hasT {
		layout = dateTimeJSON
	} else if tzOffset {
		layout = dateTimeJSONNoT
	} else if hasT {
		layout = utcDateTimeJSON
	} else {
		layout = utcDateTimeJSONNoT
	}
	return r.parse(layout, string(data))
}

// UnmarshalText decodes the textual representation of a RFC3339.
func (r *RFC3339) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		// empty XML element means no value
		return nil
	}
	tzOffset := tzOffsetRegex.Match(data)
	hasT := strings.Contains(string(data), "T") || strings.Contains(string(data), "t")
	var layout string
	if tzOffset && hasT {
		layout = time.RFC3339Nano
	} else if tzOffset {
		layout = dateTimeNoT
	} else if hasT {
		layout = utcDateTime
	} else {
		layout = utcDateTimeNoT
	}
	return r.parse(layout, string(data))
}

// parse parses a timestamp string using the specified layout.
func (r *RFC3339) parse(layout, value string) error {
	t, err := time.Parse(layout, strings.ToUpper(value))
	*r = RFC3339(t)
	return err
}

// String returns the string of the RFC3339.
func (r RFC3339) String() string {
	return time.Time(r).Format(time.RFC3339Nano)
}
