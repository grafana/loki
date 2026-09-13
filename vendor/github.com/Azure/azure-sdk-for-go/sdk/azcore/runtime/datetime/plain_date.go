// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package datetime

import (
	"time"
)

const (
	plainDate     = "2006-01-02"
	plainDateJSON = `"` + plainDate + `"`
)

// PlainDate represents a date value without time information in YYYY-MM-DD format.
// It wraps time.Time and can be marshaled to and unmarshaled from JSON.
type PlainDate time.Time

// MarshalJSON marshals the PlainDate to a JSON byte slice.
func (p PlainDate) MarshalJSON() ([]byte, error) {
	return []byte(time.Time(p).Format(plainDateJSON)), nil
}

// MarshalText returns a textual representation of PlainDate.
func (p PlainDate) MarshalText() ([]byte, error) {
	return []byte(time.Time(p).Format(plainDate)), nil
}

// UnmarshalJSON unmarshals a JSON byte slice into a PlainDate.
func (p *PlainDate) UnmarshalJSON(data []byte) (err error) {
	t, err := time.Parse(plainDateJSON, string(data))
	*p = (PlainDate)(t)
	return err
}

// UnmarshalText decodes the textual representation of PlainDate.
func (p *PlainDate) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		// empty XML element means no value
		return nil
	}
	t, err := time.Parse(plainDate, string(data))
	*p = PlainDate(t)
	return err
}

// String returns the string representation of PlainDate.
func (p PlainDate) String() string {
	return time.Time(p).Format(plainDate)
}
