package koanf

import (
	"fmt"
	"time"
)

// Int64 returns the int64 value of a given key path or 0 if the path
// does not exist or if the value is not a valid int64.
func (ko *Koanf) Int64(path string) int64 {
	if v := ko.Get(path); v != nil {
		i, _ := toInt64(v)
		return i
	}
	return 0
}

// MustInt64 returns the int64 value of a given key path or panics
// if the value is not set or set to default value of 0.
func (ko *Koanf) MustInt64(path string) int64 {
	val := ko.Int64(path)
	if val == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Int64s returns the []int64 slice value of a given key path or an
// empty []int64 slice if the path does not exist or if the value
// is not a valid int slice.
func (ko *Koanf) Int64s(path string) []int64 {
	o := ko.Get(path)
	if o == nil {
		return []int64{}
	}

	var out []int64
	switch v := o.(type) {
	case []int64:
		return v
	case []int:
		out = make([]int64, 0, len(v))
		for _, vi := range v {
			i, err := toInt64(vi)

			// On error, return as it's not a valid
			// int slice.
			if err != nil {
				return []int64{}
			}
			out = append(out, i)
		}
		return out
	case []interface{}:
		out = make([]int64, 0, len(v))
		for _, vi := range v {
			i, err := toInt64(vi)

			// On error, return as it's not a valid
			// int slice.
			if err != nil {
				return []int64{}
			}
			out = append(out, i)
		}
		return out
	}

	return []int64{}
}

// MustInt64s returns the []int64 slice value of a given key path or panics
// if the value is not set or its default value.
func (ko *Koanf) MustInt64s(path string) []int64 {
	val := ko.Int64s(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Int64Map returns the map[string]int64 value of a given key path
// or an empty map[string]int64 if the path does not exist or if the
// value is not a valid int64 map.
func (ko *Koanf) Int64Map(path string) map[string]int64 {
	var (
		out = map[string]int64{}
		o   = ko.Get(path)
	)
	if o == nil {
		return out
	}

	mp, ok := o.(map[string]interface{})
	if !ok {
		return out
	}

	out = make(map[string]int64, len(mp))
	for k, v := range mp {
		switch i := v.(type) {
		case int64:
			out[k] = i
		default:
			// Attempt a conversion.
			iv, err := toInt64(i)
			if err != nil {
				return map[string]int64{}
			}
			out[k] = iv
		}
	}
	return out
}

// MustInt64Map returns the map[string]int64 value of a given key path
// or panics if it isn't set or set to default value.
func (ko *Koanf) MustInt64Map(path string) map[string]int64 {
	val := ko.Int64Map(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Int returns the int value of a given key path or 0 if the path
// does not exist or if the value is not a valid int.
func (ko *Koanf) Int(path string) int {
	return int(ko.Int64(path))
}

// MustInt returns the int value of a given key path or panics
// if it isn't set or set to default value of 0.
func (ko *Koanf) MustInt(path string) int {
	val := ko.Int(path)
	if val == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Ints returns the []int slice value of a given key path or an
// empty []int slice if the path does not exist or if the value
// is not a valid int slice.
func (ko *Koanf) Ints(path string) []int {
	o := ko.Get(path)
	if o == nil {
		return []int{}
	}

	var out []int
	switch v := o.(type) {
	case []int:
		return v
	case []int64:
		out = make([]int, 0, len(v))
		for _, vi := range v {
			out = append(out, int(vi))
		}
		return out
	case []interface{}:
		out = make([]int, 0, len(v))
		for _, vi := range v {
			i, err := toInt64(vi)

			// On error, return as it's not a valid
			// int slice.
			if err != nil {
				return []int{}
			}
			out = append(out, int(i))
		}
		return out
	}

	return []int{}
}

// MustInts returns the []int slice value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustInts(path string) []int {
	val := ko.Ints(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// IntMap returns the map[string]int value of a given key path
// or an empty map[string]int if the path does not exist or if the
// value is not a valid int map.
func (ko *Koanf) IntMap(path string) map[string]int {
	var (
		mp  = ko.Int64Map(path)
		out = make(map[string]int, len(mp))
	)
	for k, v := range mp {
		out[k] = int(v)
	}
	return out
}

// MustIntMap returns the map[string]int value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustIntMap(path string) map[string]int {
	val := ko.IntMap(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Float64 returns the float64 value of a given key path or 0 if the path
// does not exist or if the value is not a valid float64.
func (ko *Koanf) Float64(path string) float64 {
	if v := ko.Get(path); v != nil {
		f, _ := toFloat64(v)
		return f
	}
	return 0
}

// MustFloat64 returns the float64 value of a given key path or panics
// if it isn't set or set to default value 0.
func (ko *Koanf) MustFloat64(path string) float64 {
	val := ko.Float64(path)
	if val == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Float64s returns the []float64 slice value of a given key path or an
// empty []float64 slice if the path does not exist or if the value
// is not a valid float64 slice.
func (ko *Koanf) Float64s(path string) []float64 {
	o := ko.Get(path)
	if o == nil {
		return []float64{}
	}

	var out []float64
	switch v := o.(type) {
	case []float64:
		return v
	case []interface{}:
		out = make([]float64, 0, len(v))
		for _, vi := range v {
			i, err := toFloat64(vi)

			// On error, return as it's not a valid
			// int slice.
			if err != nil {
				return []float64{}
			}
			out = append(out, i)
		}
		return out
	}

	return []float64{}
}

// MustFloat64s returns the []Float64 slice value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustFloat64s(path string) []float64 {
	val := ko.Float64s(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Float64Map returns the map[string]float64 value of a given key path
// or an empty map[string]float64 if the path does not exist or if the
// value is not a valid float64 map.
func (ko *Koanf) Float64Map(path string) map[string]float64 {
	var (
		out = map[string]float64{}
		o   = ko.Get(path)
	)
	if o == nil {
		return out
	}

	mp, ok := o.(map[string]interface{})
	if !ok {
		return out
	}

	out = make(map[string]float64, len(mp))
	for k, v := range mp {
		switch i := v.(type) {
		case float64:
			out[k] = i
		default:
			// Attempt a conversion.
			iv, err := toFloat64(i)
			if err != nil {
				return map[string]float64{}
			}
			out[k] = iv
		}
	}
	return out
}

// MustFloat64Map returns the map[string]float64 value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustFloat64Map(path string) map[string]float64 {
	val := ko.Float64Map(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Duration returns the time.Duration value of a given key path assuming
// that the key contains a valid numeric value.
func (ko *Koanf) Duration(path string) time.Duration {
	// Look for a parsable string representation first.
	if v := ko.Int64(path); v != 0 {
		return time.Duration(v)
	}

	v, _ := time.ParseDuration(ko.String(path))
	return v
}

// MustDuration returns the time.Duration value of a given key path or panics
// if it isn't set or set to default value 0.
func (ko *Koanf) MustDuration(path string) time.Duration {
	val := ko.Duration(path)
	if val == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Time attempts to parse the value of a given key path and return time.Time
// representation. If the value is numeric, it is treated as a UNIX timestamp
// and if it's string, a parse is attempted with the given layout.
func (ko *Koanf) Time(path, layout string) time.Time {
	// Unix timestamp?
	v := ko.Int64(path)
	if v != 0 {
		return time.Unix(v, 0)
	}

	// String representation.
	s := ko.String(path)
	if s != "" {
		t, _ := time.Parse(layout, s)
		return t
	}

	return time.Time{}
}

// MustTime attempts to parse the value of a given key path and return time.Time
// representation. If the value is numeric, it is treated as a UNIX timestamp
// and if it's string, a parse is attempted with the given layout. It panics if
// the parsed time is zero.
func (ko *Koanf) MustTime(path, layout string) time.Time {
	val := ko.Time(path, layout)
	if val.IsZero() {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// String returns the string value of a given key path or "" if the path
// does not exist or if the value is not a valid string.
func (ko *Koanf) String(path string) string {
	if v := ko.Get(path); v != nil {
		if i, ok := v.(string); ok {
			return i
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// MustString returns the string value of a given key path
// or panics if it isn't set or set to default value "".
func (ko *Koanf) MustString(path string) string {
	val := ko.String(path)
	if val == "" {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Strings returns the []string slice value of a given key path or an
// empty []string slice if the path does not exist or if the value
// is not a valid string slice.
func (ko *Koanf) Strings(path string) []string {
	o := ko.Get(path)
	if o == nil {
		return []string{}
	}

	var out []string
	switch v := o.(type) {
	case []interface{}:
		out = make([]string, 0, len(v))
		for _, u := range v {
			if s, ok := u.(string); ok {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprintf("%v", u))
			}
		}
		return out
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	}

	return []string{}
}

// MustStrings returns the []string slice value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustStrings(path string) []string {
	val := ko.Strings(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// StringMap returns the map[string]string value of a given key path
// or an empty map[string]string if the path does not exist or if the
// value is not a valid string map.
func (ko *Koanf) StringMap(path string) map[string]string {
	var (
		out = map[string]string{}
		o   = ko.Get(path)
	)
	if o == nil {
		return out
	}

	switch mp := o.(type) {
	case map[string]string:
		out = make(map[string]string, len(mp))
		for k, v := range mp {
			out[k] = v
		}
	case map[string]interface{}:
		out = make(map[string]string, len(mp))
		for k, v := range mp {
			switch s := v.(type) {
			case string:
				out[k] = s
			default:
				// There's a non string type. Return.
				return map[string]string{}
			}
		}
	}

	return out
}

// MustStringMap returns the map[string]string value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustStringMap(path string) map[string]string {
	val := ko.StringMap(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// StringsMap returns the map[string][]string value of a given key path
// or an empty map[string][]string if the path does not exist or if the
// value is not a valid strings map.
func (ko *Koanf) StringsMap(path string) map[string][]string {
	var (
		out = map[string][]string{}
		o   = ko.Get(path)
	)
	if o == nil {
		return out
	}

	switch mp := o.(type) {
	case map[string][]string:
		out = make(map[string][]string, len(mp))
		for k, v := range mp {
			out[k] = append(out[k], v...)
		}
	case map[string][]interface{}:
		out = make(map[string][]string, len(mp))
		for k, v := range mp {
			for _, v := range v {
				switch sv := v.(type) {
				case string:
					out[k] = append(out[k], sv)
				default:
					return map[string][]string{}
				}
			}
		}
	case map[string]interface{}:
		out = make(map[string][]string, len(mp))
		for k, v := range mp {
			switch s := v.(type) {
			case []string:
				out[k] = append(out[k], s...)
			case []interface{}:
				for _, v := range s {
					switch sv := v.(type) {
					case string:
						out[k] = append(out[k], sv)
					default:
						return map[string][]string{}
					}
				}
			default:
				// There's a non []interface type. Return.
				return map[string][]string{}
			}
		}
	}

	return out
}

// MustStringsMap returns the map[string][]string value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustStringsMap(path string) map[string][]string {
	val := ko.StringsMap(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Bytes returns the []byte value of a given key path or an empty
// []byte slice if the path does not exist or if the value is not a valid string.
func (ko *Koanf) Bytes(path string) []byte {
	return []byte(ko.String(path))
}

// MustBytes returns the []byte value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustBytes(path string) []byte {
	val := ko.Bytes(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// Bool returns the bool value of a given key path or false if the path
// does not exist or if the value is not a valid bool representation.
// Accepted string representations of bool are the ones supported by strconv.ParseBool.
func (ko *Koanf) Bool(path string) bool {
	if v := ko.Get(path); v != nil {
		b, _ := toBool(v)
		return b
	}
	return false
}

// Bools returns the []bool slice value of a given key path or an
// empty []bool slice if the path does not exist or if the value
// is not a valid bool slice.
func (ko *Koanf) Bools(path string) []bool {
	o := ko.Get(path)
	if o == nil {
		return []bool{}
	}

	var out []bool
	switch v := o.(type) {
	case []interface{}:
		out = make([]bool, 0, len(v))
		for _, u := range v {
			b, err := toBool(u)
			if err != nil {
				return nil
			}
			out = append(out, b)
		}
		return out
	case []bool:
		return out
	}
	return nil
}

// MustBools returns the []bool value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustBools(path string) []bool {
	val := ko.Bools(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}

// BoolMap returns the map[string]bool value of a given key path
// or an empty map[string]bool if the path does not exist or if the
// value is not a valid bool map.
func (ko *Koanf) BoolMap(path string) map[string]bool {
	var (
		out = map[string]bool{}
		o   = ko.Get(path)
	)
	if o == nil {
		return out
	}

	mp, ok := o.(map[string]interface{})
	if !ok {
		return out
	}
	out = make(map[string]bool, len(mp))
	for k, v := range mp {
		switch i := v.(type) {
		case bool:
			out[k] = i
		default:
			// Attempt a conversion.
			b, err := toBool(i)
			if err != nil {
				return map[string]bool{}
			}
			out[k] = b
		}
	}

	return out
}

// MustBoolMap returns the map[string]bool value of a given key path or panics
// if the value is not set or set to default value.
func (ko *Koanf) MustBoolMap(path string) map[string]bool {
	val := ko.BoolMap(path)
	if len(val) == 0 {
		panic(fmt.Sprintf("invalid value: %s=%v", path, val))
	}
	return val
}
