package retention

import (
	"reflect"
	"unsafe"
)

// unsafeGetString is like yolostring but with a meaningful name
func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}

func unsafeGetBytes(s string) []byte {
	var buf []byte
	p := unsafe.Pointer(&buf)
	*(*string)(p) = s
	(*reflect.SliceHeader)(p).Cap = len(s)
	return buf
}

// Build an index key, encoded as multiple parts separated by a 0 byte, with extra space at the end.
func buildRangeValue(extra int, ss ...[]byte) []byte {
	length := extra
	for _, s := range ss {
		length += len(s) + 1
	}
	output, i := make([]byte, length), 0
	for _, s := range ss {
		i += copy(output[i:], s) + 1
	}
	return output
}

// Encode a complete key including type marker (which goes at the end)
func encodeRangeKey(keyType byte, ss ...[]byte) []byte {
	output := buildRangeValue(2, ss...)
	output[len(output)-2] = keyType
	return output
}
