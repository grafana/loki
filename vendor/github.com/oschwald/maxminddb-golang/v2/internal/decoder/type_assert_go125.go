//go:build go1.25

package decoder

import (
	"reflect"
)

// tryTypeAssert attempts to type assert a reflect.Value to the Unmarshaler interface.
// In Go 1.25+, this uses reflect.TypeAssert which avoids allocations compared to
// the traditional Interface().(Type) approach. The value should be the address of the
// struct since UnmarshalMaxMindDB implementations use pointer receivers.
//
//go:inline
func tryTypeAssert(v reflect.Value) (Unmarshaler, bool) {
	return reflect.TypeAssert[Unmarshaler](v)
}
