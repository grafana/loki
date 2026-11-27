package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	minimumTick  = time.Millisecond
	second       = int64(time.Second / minimumTick)
	nanosPerTick = int64(minimumTick / time.Nanosecond)
)

// The number of digits after the dot.
var dotPrecision = int(math.Log10(float64(second)))

type Time int64

// String returns a string representation of the Time.
func (t Time) String() string {
	return strconv.FormatFloat(float64(t)/float64(second), 'f', -1, 64)
}

// MarshalJSON implements the json.Marshaler interface.
func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(t.String()), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (t *Time) UnmarshalJSON(b []byte) error {
	p := strings.Split(string(b), ".")
	switch len(p) {
	case 1:
		v, err := strconv.ParseInt(p[0], 10, 64)
		if err != nil {
			return err
		}
		*t = Time(v * second)

	case 2:
		v, err := strconv.ParseInt(p[0], 10, 64)
		if err != nil {
			return err
		}
		v *= second

		prec := dotPrecision - len(p[1])
		if prec < 0 {
			p[1] = p[1][:dotPrecision]
		} else if prec > 0 {
			p[1] = p[1] + strings.Repeat("0", prec)
		}

		va, err := strconv.ParseInt(p[1], 10, 32)
		if err != nil {
			return err
		}

		// If the value was something like -0.1 the negative is lost in the
		// parsing because of the leading zero, this ensures that we capture it.
		if len(p[0]) > 0 && p[0][0] == '-' && v+va > 0 {
			*t = Time(v+va) * -1
		} else {
			*t = Time(v + va)
		}

	default:
		return fmt.Errorf("invalid time %q", string(b))
	}
	return nil
}

// Time returns the time.Time representation of t.
func (t Time) Time() time.Time {
	return time.Unix(int64(t)/second, (int64(t)%second)*nanosPerTick)
}
