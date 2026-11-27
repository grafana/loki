package jsonparser

// Append adds value to the end of the JSON array addressed by keys.
//
// When keys is empty, Append addresses the top-level value. If a keyed path
// does not exist, Append creates it as a single-element array using Set's
// auto-vivification behavior.
//
// SYS-REQ-009, SYS-REQ-110
func Append(data []byte, value []byte, keys ...string) ([]byte, error) {
	_, dataType, startOffset, endOffset, err := internalGet(data, keys...)
	if err != nil {
		if err != KeyPathNotFoundError || len(keys) == 0 {
			return nil, err
		}

		arrayValue := make([]byte, len(value)+2)
		offset := WriteToBuffer(arrayValue, "[")
		offset += copy(arrayValue[offset:], value)
		WriteToBuffer(arrayValue[offset:], "]")

		return Set(data, arrayValue, keys...)
	}

	if dataType != Array {
		return nil, MalformedArrayError
	}

	closeOffset := endOffset - 1
	if closeOffset <= startOffset || closeOffset >= len(data) || data[closeOffset] != ']' {
		return nil, MalformedArrayError
	}

	hasElements := nextToken(data[startOffset+1:closeOffset]) != -1
	extraSpace := len(value)
	if hasElements {
		extraSpace++
	}

	result := make([]byte, len(data)+extraSpace)
	offset := copy(result, data[:closeOffset])
	if hasElements {
		offset += WriteToBuffer(result[offset:], ",")
	}
	offset += copy(result[offset:], value)
	copy(result[offset:], data[closeOffset:])

	return result, nil
}
