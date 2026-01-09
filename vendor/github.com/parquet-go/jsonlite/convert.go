package jsonlite

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// Convertible is the type constraint for the As function, defining all supported
// conversion types.
type Convertible interface {
	any |
		bool |
		int64 |
		uint64 |
		float64 |
		json.Number |
		string |
		time.Duration |
		time.Time |
		[]any |
		[]bool |
		[]int64 |
		[]uint64 |
		[]float64 |
		[]json.Number |
		[]string |
		[]time.Duration |
		[]time.Time |
		map[string]any |
		map[string]bool |
		map[string]int64 |
		map[string]uint64 |
		map[string]float64 |
		map[string]json.Number |
		map[string]string |
		map[string]time.Duration |
		map[string]time.Time
}

// As converts a JSON value to the specified Go type.
//
// For primitive types (bool, int64, uint64, float64, string, time.Duration, time.Time):
//   - Follows the same conversion rules as the corresponding As* function
//   - Returns zero value for nil or incompatible types
//
// For json.Number:
//   - Returns the number as a json.Number string
//   - Returns empty string for non-number values
//
// For slice types ([]T):
//   - Converts JSON arrays where each element is converted using the primitive T logic
//   - Returns nil for non-array values
//   - Returns empty slice for empty arrays
//
// For map types (map[string]T):
//   - Converts JSON objects where each value is converted using the primitive T logic
//   - Returns nil for non-object values
//   - Returns empty map for empty objects
//
// For any types:
//   - any: Returns the most natural Go representation (bool, int64, uint64, float64, string, []any, map[string]any)
//   - []any: Recursively converts array elements to any
//   - map[string]any: Recursively converts object values to any
//
// Examples:
//
//	val, _ := Parse(`[1, 2, 3]`)
//	nums := As[[]int64](val)  // []int64{1, 2, 3}
//
//	val, _ := Parse(`{"a": 1, "b": 2}`)
//	m := As[map[string]int64](val)  // map[string]int64{"a": 1, "b": 2}
func As[T Convertible](v *Value) T {
	switch any(*new(T)).(type) {
	case bool:
		return any(asBool(v)).(T)
	case int64:
		return any(asInt(v)).(T)
	case uint64:
		return any(asUint(v)).(T)
	case float64:
		return any(asFloat(v)).(T)
	case json.Number:
		return any(asNumber(v)).(T)
	case string:
		return any(asString(v)).(T)
	case time.Duration:
		return any(asDuration(v)).(T)
	case time.Time:
		return any(asTime(v)).(T)
	case []any:
		return any(asSlice(v, asAny)).(T)
	case []bool:
		return any(asSlice(v, asBool)).(T)
	case []int64:
		return any(asSlice(v, asInt)).(T)
	case []uint64:
		return any(asSlice(v, asUint)).(T)
	case []float64:
		return any(asSlice(v, asFloat)).(T)
	case []json.Number:
		return any(asSlice(v, asNumber)).(T)
	case []string:
		return any(asSlice(v, asString)).(T)
	case []time.Duration:
		return any(asSlice(v, asDuration)).(T)
	case []time.Time:
		return any(asSlice(v, asTime)).(T)
	case map[string]any:
		return any(asMap(v, asAny)).(T)
	case map[string]bool:
		return any(asMap(v, asBool)).(T)
	case map[string]int64:
		return any(asMap(v, asInt)).(T)
	case map[string]uint64:
		return any(asMap(v, asUint)).(T)
	case map[string]float64:
		return any(asMap(v, asFloat)).(T)
	case map[string]json.Number:
		return any(asMap(v, asNumber)).(T)
	case map[string]string:
		return any(asMap(v, asString)).(T)
	case map[string]time.Duration:
		return any(asMap(v, asDuration)).(T)
	case map[string]time.Time:
		return any(asMap(v, asTime)).(T)
	default:
		// Special handling for any to avoid panic when result is nil
		r, _ := asAny(v).(T)
		return r
	}
}

// asBool coerces the value to a boolean.
func asBool(v *Value) bool {
	if v != nil {
		switch v.Kind() {
		case True:
			return true
		case Number:
			f, err := strconv.ParseFloat(v.json(), 64)
			return err == nil && f != 0
		case String, Object, Array:
			return v.Len() > 0
		}
	}
	return false
}

// asString coerces the value to a string.
func asString(v *Value) string {
	if v != nil && v.Kind() != Null {
		return v.String()
	}
	return ""
}

// asInt coerces the value to a signed 64-bit integer.
func asInt(v *Value) int64 {
	if v != nil {
		switch v.Kind() {
		case True:
			return 1
		case Number:
			if i, err := strconv.ParseInt(v.json(), 10, 64); err == nil {
				return i
			}
			if f, err := strconv.ParseFloat(v.json(), 64); err == nil {
				return int64(f)
			}
		case String:
			// Strip surrounding quotes - no escapes in valid number strings
			s := v.json()
			s = s[1 : len(s)-1]
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return i
			}
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return int64(f)
			}
		}
	}
	return 0
}

// asUint coerces the value to an unsigned 64-bit integer.
func asUint(v *Value) uint64 {
	if v != nil {
		switch v.Kind() {
		case True:
			return 1
		case Number:
			if u, err := strconv.ParseUint(v.json(), 10, 64); err == nil {
				return u
			}
			if f, err := strconv.ParseFloat(v.json(), 64); err == nil {
				if f >= 0 {
					return uint64(f)
				}
			}
		case String:
			// Strip surrounding quotes - no escapes in valid number strings
			s := v.json()
			s = s[1 : len(s)-1]
			if u, err := strconv.ParseUint(s, 10, 64); err == nil {
				return u
			}
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				if f >= 0 {
					return uint64(f)
				}
			}
		}
	}
	return 0
}

// asFloat coerces the value to a 64-bit floating point number.
func asFloat(v *Value) float64 {
	if v != nil {
		switch v.Kind() {
		case True:
			return 1
		case Number:
			if f, err := strconv.ParseFloat(v.json(), 64); err == nil {
				return f
			}
		case String:
			// Strip surrounding quotes - no escapes in valid number strings
			s := v.json()
			s = s[1 : len(s)-1]
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

// asDuration coerces the value to a time.Duration.
func asDuration(v *Value) time.Duration {
	if v != nil {
		switch v.Kind() {
		case True:
			return time.Second
		case Number:
			if f, err := strconv.ParseFloat(v.json(), 64); err == nil {
				return time.Duration(f * float64(time.Second))
			}
		case String:
			// Strip surrounding quotes - no escapes in valid duration strings
			s := v.json()
			s = s[1 : len(s)-1]
			if d, err := time.ParseDuration(s); err == nil {
				return d
			}
		}
	}
	return 0
}

// asTime coerces the value to a time.Time.
func asTime(v *Value) time.Time {
	if v != nil {
		switch v.Kind() {
		case Number:
			if f, err := strconv.ParseFloat(v.json(), 64); err == nil {
				sec, frac := math.Modf(f)
				return time.Unix(int64(sec), int64(frac*1e9)).UTC()
			}
		case String:
			// Strip surrounding quotes - no escapes in valid RFC3339 time strings
			s := v.json()
			s = s[1 : len(s)-1]
			if t, err := time.ParseInLocation(time.RFC3339, s, time.UTC); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// asNumber coerces the value to a json.Number.
func asNumber(v *Value) json.Number {
	if v != nil && v.Kind() == Number {
		return json.Number(v.json())
	}
	return ""
}

// asSlice converts a JSON array to a Go slice by applying the converter
// function to each element. Returns nil for non-array values.
func asSlice[E any](v *Value, converter func(*Value) E) []E {
	if v == nil || v.Kind() != Array {
		return nil
	}
	result := make([]E, 0, v.Len())
	for elem := range v.Array {
		result = append(result, converter(elem))
	}
	return result
}

// asMap converts a JSON object to a Go map by applying the converter
// function to each value. Returns nil for non-object values.
func asMap[V any](v *Value, converter func(*Value) V) map[string]V {
	if v == nil || v.Kind() != Object {
		return nil
	}
	result := make(map[string]V, v.Len())
	for k, val := range v.Object {
		result[k] = converter(val)
	}
	return result
}

// asAny converts a JSON value to its most natural Go representation.
func asAny(v *Value) any {
	if v == nil {
		return nil
	}
	switch v.Kind() {
	case Null:
		return nil
	case True:
		return true
	case False:
		return false
	case Number:
		switch v.NumberType() {
		case Int:
			return v.Int()
		case Uint:
			u := v.Uint()
			if u <= math.MaxInt64 {
				return int64(u)
			}
			return u
		default:
			return v.Float()
		}
	case String:
		return v.String()
	case Array:
		return asSlice(v, asAny)
	case Object:
		return asMap(v, asAny)
	default:
		return nil
	}
}
