package xcap

import "math"

// value stores an observation value without interface boxing. Its
// interpretation is defined by the [DataType] of the associated [Statistic].
type value struct {
	bits uint64
}

func int64Value(v int64) value {
	return value{bits: uint64(v)}
}

func float64Value(v float64) value {
	return value{bits: math.Float64bits(v)}
}

func boolValue(v bool) value {
	if v {
		return value{bits: 1}
	}
	return value{}
}

// valueFromBits wraps a raw wire bit pattern. The referenced statistic's data
// type defines how the bits are later interpreted.
func valueFromBits(bits uint64) value {
	return value{bits: bits}
}

func (v value) int64() int64 {
	return int64(v.bits)
}

func (v value) float64() float64 {
	return math.Float64frombits(v.bits)
}

func (v value) bool() bool {
	return v.bits != 0
}
