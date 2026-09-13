// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package datetime

import (
	"strings"
	"time"
)

const (
	rfc1123JSON = `"` + time.RFC1123 + `"`
)

// RFC1123 represents a date and time value in RFC 1123 format as defined in
// https://tools.ietf.org/html/rfc1123.
type RFC1123 time.Time

// MarshalJSON marshals the RFC1123 timestamp to a JSON byte slice.
func (r RFC1123) MarshalJSON() ([]byte, error) {
	return []byte(time.Time(r).Format(rfc1123JSON)), nil
}

// MarshalText returns a textual representation of RFC1123.
func (r RFC1123) MarshalText() ([]byte, error) {
	return []byte(time.Time(r).Format(time.RFC1123)), nil
}

// UnmarshalJSON unmarshals a JSON byte slice into an RFC1123 timestamp.
func (r *RFC1123) UnmarshalJSON(data []byte) error {
	t, err := time.Parse(rfc1123JSON, strings.ToUpper(string(data)))
	*r = RFC1123(t)
	return err
}

// UnmarshalText decodes the textual representation of RFC1123.
func (r *RFC1123) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		// empty XML element means no value
		return nil
	}
	t, err := time.Parse(time.RFC1123, string(data))
	*r = RFC1123(t)
	return err
}

// String returns the string of RFC1123.
func (r RFC1123) String() string {
	return time.Time(r).Format(time.RFC1123)
}
