package jsonparser

// SYS-REQ-006, SYS-REQ-028, SYS-REQ-029, SYS-REQ-052, SYS-REQ-053, SYS-REQ-055, SYS-REQ-083
// EachArray is the canonical name; ArrayEach is kept for backward compatibility.
func EachArray(data []byte, cb func(value []byte, dataType ValueType, offset int, err error), keys ...string) {
	ArrayEach(data, cb, keys...)
}

// SYS-REQ-007, SYS-REQ-030, SYS-REQ-031, SYS-REQ-032, SYS-REQ-054, SYS-REQ-084
// EachObject is the canonical name; ObjectEach is kept for backward compatibility.
func EachObject(data []byte, cb func(key []byte, value []byte, dataType ValueType, offset int) error, keys ...string) error {
	return ObjectEach(data, cb, keys...)
}

// SYS-REQ-004
// EachArrayErr is the canonical name; ArrayEachErr is kept for backward compatibility.
func EachArrayErr(data []byte, cb func(value []byte, dataType ValueType, offset int, err error) error, keys ...string) (int, error) {
	return ArrayEachErr(data, cb, keys...)
}

// SYS-REQ-113
// EachArrayWildcard is the canonical name; ArrayEachWildcard is kept for backward compatibility.
func EachArrayWildcard(data []byte, cb func(idx int, value []byte, vt ValueType, offset int, err error) error, keys ...string) (int, error) {
	return ArrayEachWildcard(data, cb, keys...)
}
