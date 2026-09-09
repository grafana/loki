package jsonparser

import "strconv"

const wildcardPathComponent = "[*]"

// EachKeyWildcard resolves a path that may contain [*] components. A wildcard
// requires an array at that position and fans out over its elements before
// resolving the rest of the path. Multiple wildcards produce one callback for
// every matching combination, in document order.
//
// idx is the zero-based order in which matches are reported.
//
// SYS-REQ-113
func EachKeyWildcard(data []byte, cb func(idx int, value []byte, vt ValueType, err error), path ...string) error {
	nextMatch := 0
	return eachKeyWildcard(data, cb, &nextMatch, path)
}

// SYS-REQ-113
func eachKeyWildcard(data []byte, cb func(idx int, value []byte, vt ValueType, err error), nextMatch *int, path []string) error {
	wildcard := wildcardIndex(path)
	if wildcard == -1 {
		value, valueType, _, err := Get(data, path...)
		if err != nil {
			// A missing path has no value to report. Parsing errors for a
			// located terminal value, however, follow EachKey's convention
			// and are delivered to the callback as well as returned.
			if err != KeyPathNotFoundError {
				cb(*nextMatch, value, valueType, err)
				(*nextMatch)++
			}
			return err
		}

		cb(*nextMatch, value, valueType, nil)
		(*nextMatch)++
		return nil
	}

	array, valueType, _, err := Get(data, path[:wildcard]...)
	if err != nil {
		return err
	}
	if valueType != Array {
		return MalformedArrayError
	}

	remaining := path[wildcard+1:]
	_, err = ArrayEachErr(array, func(value []byte, elementType ValueType, _ int, parseErr error) error {
		if parseErr != nil {
			cb(*nextMatch, value, elementType, parseErr)
			(*nextMatch)++
			return parseErr
		}

		if len(remaining) == 0 {
			cb(*nextMatch, value, elementType, nil)
			(*nextMatch)++
			return nil
		}

		return eachKeyWildcard(value, cb, nextMatch, remaining)
	})
	return err
}

// ArrayEachWildcard iterates over an array addressed by keys. A terminal [*]
// component is accepted as an explicit request for all elements; without it,
// the function behaves like ArrayEachErr.
// SYS-REQ-113
func ArrayEachWildcard(data []byte, cb func(idx int, value []byte, vt ValueType, offset int, err error) error, keys ...string) (int, error) {
	if len(keys) > 0 && keys[len(keys)-1] == wildcardPathComponent {
		keys = keys[:len(keys)-1]
	}

	index := 0
	return ArrayEachErr(data, func(value []byte, valueType ValueType, offset int, err error) error {
		current := index
		index++
		return cb(current, value, valueType, offset, err)
	}, keys...)
}

// SetWildcard applies Set to every concrete path matched by [*]. Wildcards
// may appear at any array position and may be repeated. If a wildcard matches
// an empty array, data is returned unchanged.
//
// SYS-REQ-113
func SetWildcard(data []byte, setValue []byte, keys ...string) ([]byte, error) {
	if wildcardIndex(keys) == -1 {
		return Set(data, setValue, keys...)
	}

	var paths [][]string
	if err := expandWildcardPaths(data, nil, keys, &paths); err != nil {
		return nil, err
	}

	result := data
	for _, path := range paths {
		var err error
		result, err = Set(result, setValue, path...)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// SYS-REQ-113
func expandWildcardPaths(data []byte, concrete, remaining []string, paths *[][]string) error {
	wildcard := wildcardIndex(remaining)
	if wildcard == -1 {
		path := make([]string, len(concrete)+len(remaining))
		copy(path, concrete)
		copy(path[len(concrete):], remaining)
		*paths = append(*paths, path)
		return nil
	}

	arrayPath := make([]string, len(concrete)+wildcard)
	copy(arrayPath, concrete)
	copy(arrayPath[len(concrete):], remaining[:wildcard])

	array, valueType, _, err := Get(data, arrayPath...)
	if err != nil {
		return err
	}
	if valueType != Array {
		return MalformedArrayError
	}

	index := 0
	_, err = ArrayEachErr(array, func(_ []byte, _ ValueType, _ int, parseErr error) error {
		if parseErr != nil {
			return parseErr
		}

		nextPath := make([]string, len(arrayPath)+1)
		copy(nextPath, arrayPath)
		nextPath[len(arrayPath)] = "[" + strconv.Itoa(index) + "]"
		index++

		return expandWildcardPaths(data, nextPath, remaining[wildcard+1:], paths)
	})
	return err
}

// SYS-REQ-113
func wildcardIndex(path []string) int {
	for i, component := range path {
		if component == wildcardPathComponent {
			return i
		}
	}
	return -1
}
