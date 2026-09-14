// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package datetime

import (
	"strings"
	"time"
)

// used to format timestamps with a fixed GMT zone name before sending across the wire
var gmt = time.FixedZone("GMT", 0)

// RFC7231 represents a date and time value in RFC 1123 format as defined in
// https://tools.ietf.org/html/rfc1123.
// The timezone is set to GMT as required by RFC 7231 HTTP-date / IMF-fixdate:
// https://datatracker.ietf.org/doc/html/rfc7231#section-7.1.1.1.
type RFC7231 time.Time

// MarshalJSON marshals the RFC7231 timestamp to a JSON byte slice.
func (r RFC7231) MarshalJSON() ([]byte, error) {
	return []byte(time.Time(r).In(gmt).Format(rfc1123JSON)), nil
}

// MarshalText returns a textual representation of RFC7231.
func (r RFC7231) MarshalText() ([]byte, error) {
	return []byte(time.Time(r).In(gmt).Format(time.RFC1123)), nil
}

// UnmarshalJSON unmarshals a JSON byte slice into an RFC7231 timestamp.
func (r *RFC7231) UnmarshalJSON(data []byte) error {
	t, err := time.Parse(rfc1123JSON, strings.ToUpper(string(data)))
	*r = RFC7231(t.UTC())
	return err
}

// UnmarshalText decodes the textual representation of RFC7231.
func (r *RFC7231) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		// empty XML element means no value
		return nil
	}
	t, err := time.Parse(time.RFC1123, string(data))
	*r = RFC7231(t.UTC())
	return err
}

// String returns the string of RFC7231.
func (r RFC7231) String() string {
	return time.Time(r).In(gmt).Format(time.RFC1123)
}
