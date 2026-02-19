package orderedmap

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/buger/jsonparser"
)

var (
	_ json.Marshaler   = &OrderedMap[int, any]{}
	_ json.Unmarshaler = &OrderedMap[int, any]{}
)

// MarshalJSON implements the json.Marshaler interface.
func (om *OrderedMap[K, V]) MarshalJSON() ([]byte, error) { //nolint:funlen
	if om == nil || om.list == nil {
		return []byte("null"), nil
	}

	buf := &bytes.Buffer{}
	buf.WriteByte('{')

	for pair, firstIteration := om.Oldest(), true; pair != nil; pair = pair.Next() {
		if firstIteration {
			firstIteration = false
		} else {
			buf.WriteByte(',')
		}

		// Marshal the key
		switch key := any(pair.Key).(type) {
		case string:
			if err := writeJSONString(buf, key, om.disableHTMLEscape); err != nil {
				return nil, err
			}
		case encoding.TextMarshaler:
			buf.WriteByte('"')
			text, err := key.MarshalText()
			if err != nil {
				return nil, err
			}
			buf.Write(text)
			buf.WriteByte('"')
		case int:
			buf.WriteByte('"')
			buf.WriteString(strconv.Itoa(key))
			buf.WriteByte('"')
		case int8:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatInt(int64(key), 10))
			buf.WriteByte('"')
		case int16:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatInt(int64(key), 10))
			buf.WriteByte('"')
		case int32:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatInt(int64(key), 10))
			buf.WriteByte('"')
		case int64:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatInt(key, 10))
			buf.WriteByte('"')
		case uint:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatUint(uint64(key), 10))
			buf.WriteByte('"')
		case uint8:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatUint(uint64(key), 10))
			buf.WriteByte('"')
		case uint16:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatUint(uint64(key), 10))
			buf.WriteByte('"')
		case uint32:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatUint(uint64(key), 10))
			buf.WriteByte('"')
		case uint64:
			buf.WriteByte('"')
			buf.WriteString(strconv.FormatUint(key, 10))
			buf.WriteByte('"')
		default:
			// this switch takes care of wrapper types around primitive types, such as
			// type myType string
			switch keyValue := reflect.ValueOf(key); keyValue.Type().Kind() {
			case reflect.String:
				if err := writeJSONString(buf, keyValue.String(), om.disableHTMLEscape); err != nil {
					return nil, err
				}
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				buf.WriteByte('"')
				buf.WriteString(strconv.FormatInt(keyValue.Int(), 10))
				buf.WriteByte('"')
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				buf.WriteByte('"')
				buf.WriteString(strconv.FormatUint(keyValue.Uint(), 10))
				buf.WriteByte('"')
			default:
				return nil, fmt.Errorf("unsupported key type: %T", key)
			}
		}

		buf.WriteByte(':')

		// Marshal the value
		valueBytes, err := jsonMarshal(pair.Value, om.disableHTMLEscape)
		if err != nil {
			return nil, err
		}
		buf.Write(valueBytes)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// writeJSONString writes a JSON-encoded string to the buffer.
// It handles proper escaping according to JSON specification.
func writeJSONString(buf *bytes.Buffer, str string, disableHTMLEscape bool) error {
	// Use json.Marshal for proper escaping
	var encoded []byte
	var err error
	if disableHTMLEscape {
		// Create a temporary buffer and encoder to handle HTML escaping
		tempBuf := &bytes.Buffer{}
		encoder := json.NewEncoder(tempBuf)
		encoder.SetEscapeHTML(false)
		err = encoder.Encode(str)
		if err != nil {
			return err
		}
		// Remove the trailing newline that Encode adds
		encoded = bytes.TrimRight(tempBuf.Bytes(), "\n")
	} else {
		encoded, err = json.Marshal(str)
		if err != nil {
			return err
		}
	}
	buf.Write(encoded)
	return nil
}

func jsonMarshal(value interface{}, disableHTMLEscape bool) ([]byte, error) {
	if disableHTMLEscape {
		buffer := &bytes.Buffer{}
		encoder := json.NewEncoder(buffer)
		encoder.SetEscapeHTML(false)
		err := encoder.Encode(value)
		// Encode() adds an extra newline, strip it off to guarantee same behavior as json.Marshal
		return bytes.TrimRight(buffer.Bytes(), "\n"), err
	}
	return json.Marshal(value)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (om *OrderedMap[K, V]) UnmarshalJSON(data []byte) error {
	if om.list == nil {
		om.initialize(0, om.disableHTMLEscape)
	}

	return jsonparser.ObjectEach(
		data,
		func(keyData []byte, valueData []byte, dataType jsonparser.ValueType, offset int) error {
			if dataType == jsonparser.String {
				// jsonparser removes the enclosing quotes; we need to restore them to make a valid JSON
				valueData = data[offset-len(valueData)-2 : offset]
			}

			var key K
			var value V

			switch typedKey := any(&key).(type) {
			case *string:
				s, err := decodeUTF8(keyData)
				if err != nil {
					return err
				}
				*typedKey = s
			case encoding.TextUnmarshaler:
				if err := typedKey.UnmarshalText(keyData); err != nil {
					return err
				}
			case *int, *int8, *int16, *int32, *int64, *uint, *uint8, *uint16, *uint32, *uint64:
				if err := json.Unmarshal(keyData, typedKey); err != nil {
					return err
				}
			default:
				// this switch takes care of wrapper types around primitive types, such as
				// type myType string
				switch reflect.TypeOf(key).Kind() {
				case reflect.String:
					s, err := decodeUTF8(keyData)
					if err != nil {
						return err
					}

					convertedKeyData := reflect.ValueOf(s).Convert(reflect.TypeOf(key))
					reflect.ValueOf(&key).Elem().Set(convertedKeyData)
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					if err := json.Unmarshal(keyData, &key); err != nil {
						return err
					}
				default:
					return fmt.Errorf("unsupported key type: %T", key)
				}
			}

			if err := json.Unmarshal(valueData, &value); err != nil {
				return err
			}

			om.Set(key, value)
			return nil
		})
}

func decodeUTF8(input []byte) (string, error) {
	remaining, offset := input, 0
	runes := make([]rune, 0, len(remaining))

	for len(remaining) > 0 {
		r, size := utf8.DecodeRune(remaining)
		if r == utf8.RuneError && size <= 1 {
			return "", fmt.Errorf("not a valid UTF-8 string (at position %d): %s", offset, string(input))
		}

		runes = append(runes, r)
		remaining = remaining[size:]
		offset += size
	}

	return string(runes), nil
}
