// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package datetime

import (
	"encoding/json"
	"fmt"
	"time"
)

// Unix represents a Unix timestamp (seconds since January 1, 1970 UTC).
type Unix time.Time

// MarshalJSON marshals the Unix timestamp to a JSON byte slice.
func (u Unix) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(u).Unix())
}

// MarshalText returns a textual representation of Unix.
func (u Unix) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

// UnmarshalJSON unmarshals a JSON byte slice into a Unix timestamp.
func (u *Unix) UnmarshalJSON(data []byte) error {
	return u.parse(data)
}

// UnmarshalText decodes the textual representation of Unix.
func (u *Unix) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		// empty XML element means no value
		return nil
	}
	return u.parse(data)
}

// parses a Unix timestamp from a byte slice.
func (u *Unix) parse(data []byte) error {
	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return err
	}
	*u = Unix(time.Unix(seconds, 0))
	return nil
}

// String returns the string of Unix.
func (u Unix) String() string {
	return fmt.Sprint(time.Time(u).Unix())
}
