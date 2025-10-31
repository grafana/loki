package executor

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/buger/jsonparser"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

const (
	jsonSpacer           = '_'
	trueString           = "true"
	falseString          = "false"
	unescapeStackBufSize = 64
)

var (
	trueBytes = []byte("true")

	// the rune error replacement is rejected by Prometheus hence replacing them with space.
	removeInvalidUtf = func(r rune) rune {
		if r == utf8.RuneError {
			return 32 // rune value for space
		}
		return r
	}
)

func buildJSONColumns(input *array.String, requestedKeys []string) ([]string, []arrow.Array) {
	parseFunc := func(line string) (map[string]string, error) {
		return parseJSONLine(line, requestedKeys)
	}
	return buildColumns(input, requestedKeys, parseFunc, types.JSONParserErrorType)
}

// parseJSONLine parses a single JSON line and extracts key-value pairs
// implements ParseFunc
func parseJSONLine(line string, requestedKeys []string) (map[string]string, error) {
	// Use the refactored JSONParser for nested object handling and number conversion
	parser := newJSONParser()
	return parser.process(unsafeBytes(line), requestedKeys)
}

type jsonParser struct {
	prefixBuffer          [][]byte // buffer used to build json keys
	sanitizedPrefixBuffer []byte
}

// newJSONParser creates a JSON parser that can handle nested objects with flattening.
func newJSONParser() *jsonParser {
	return &jsonParser{
		prefixBuffer:          [][]byte{},
		sanitizedPrefixBuffer: make([]byte, 0, 64),
	}
}

// process parses a JSON line and returns key-value pairs with nested object flattening
func (j *jsonParser) process(line []byte, requestedKeys []string) (map[string]string, error) {
	result := make(map[string]string)

	// Create a set for faster requestedKeys lookup
	var requestedKeyLookup map[string]struct{}
	if len(requestedKeys) > 0 {
		requestedKeyLookup = make(map[string]struct{}, len(requestedKeys))
		for _, key := range requestedKeys {
			requestedKeyLookup[key] = struct{}{}
		}
	}

	// reset the state
	j.prefixBuffer = j.prefixBuffer[:0]

	// Parse the JSON recursively
	err := jsonparser.ObjectEach(line, func(key []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
		return j.parseObject(key, value, dataType, result, requestedKeyLookup)
	})
	// If there's an error, return empty result for consistency with malformed JSON handling
	if err != nil {
		return make(map[string]string), err
	}

	return result, nil
}

func (j *jsonParser) parseObject(key, value []byte, dataType jsonparser.ValueType, result map[string]string, requestedKeyLookup map[string]struct{}) error {
	switch dataType {
	case jsonparser.String, jsonparser.Number, jsonparser.Boolean:
		return j.parseLabelValue(key, value, dataType, result, requestedKeyLookup)
	case jsonparser.Object:
		// Handle nested objects by adding to prefix buffer
		prefixLen := len(j.prefixBuffer)
		j.prefixBuffer = append(j.prefixBuffer, key)

		// Recursively parse nested object
		err := jsonparser.ObjectEach(value, func(nestedKey []byte, nestedValue []byte, nestedType jsonparser.ValueType, _ int) error {
			return j.parseObject(nestedKey, nestedValue, nestedType, result, requestedKeyLookup)
		})

		// rollback the prefix as we exit the current object
		j.prefixBuffer = j.prefixBuffer[:prefixLen]
		return err
	case jsonparser.Array:
		// Skip arrays for now (same behavior as original)
		return nil
	case jsonparser.Null:
		// Skip null values
		return nil
	default:
		// Skip unknown types
		return nil
	}
}

func (j *jsonParser) parseLabelValue(key, value []byte, dataType jsonparser.ValueType, result map[string]string, requestedKeyLookup map[string]struct{}) error {
	// Build the full key (with flattening for nested objects)
	var keyString string
	if len(j.prefixBuffer) == 0 {
		// Top-level key case
		// key is a slice pointing to the same underlying array as the data passed to jsonparser.ObjectEach
		// nothing should mutate this data as it's the original JSON string, so it should be safe to use unsafeString
		keyString = sanitizeLabelKey(unsafeString(key), true)
	} else {
		// Nested key - build flattened key with underscore separator
		j.prefixBuffer = append(j.prefixBuffer, key)
		sanitized := j.buildSanitizedPrefixFromBuffer()
		// Make a copy to avoid buffer reuse issues, since sanitized is a slice using the same array behind j.sanitizedPrefixBuffer
		keyString = string(sanitized)
		// Remove the key we just added since we'll add it back in parseObject
		j.prefixBuffer = j.prefixBuffer[:len(j.prefixBuffer)-1]
	}

	// Filter by requested keys if provided
	if requestedKeyLookup != nil {
		if _, wantKey := requestedKeyLookup[keyString]; !wantKey {
			return nil // Skip this key
		}
	}

	// Convert the value to string based on its type
	parsedValue := parseValue(value, dataType)
	if parsedValue != "" {
		// First-wins semantics for duplicates
		_, exists := result[keyString]
		if exists {
			return nil
		}
		result[keyString] = parsedValue
	}

	return nil
}

func (j *jsonParser) buildSanitizedPrefixFromBuffer() []byte {
	j.sanitizedPrefixBuffer = j.sanitizedPrefixBuffer[:0]

	for i, part := range j.prefixBuffer {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}

		if i > 0 && len(j.sanitizedPrefixBuffer) > 0 {
			j.sanitizedPrefixBuffer = append(j.sanitizedPrefixBuffer, byte(jsonSpacer))
		}
		j.sanitizedPrefixBuffer = appendSanitized(j.sanitizedPrefixBuffer, part)
	}

	return j.sanitizedPrefixBuffer
}

func parseValue(v []byte, dataType jsonparser.ValueType) string {
	switch dataType {
	case jsonparser.String:
		return unescapeJSONString(v)
	case jsonparser.Null:
		return ""
	case jsonparser.Number:
		return string(v)
	case jsonparser.Boolean:
		if bytes.Equal(v, trueBytes) {
			return trueString
		}
		return falseString
	default:
		return ""
	}
}

func unescapeJSONString(b []byte) string {
	var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings
	bU, err := jsonparser.Unescape(b, stackbuf[:])
	if err != nil {
		return ""
	}
	res := string(bU)

	if strings.ContainsRune(res, utf8.RuneError) {
		res = strings.Map(removeInvalidUtf, res)
	}

	return res
}

// sanitizeLabelKey sanitizes a key to be a valid label name
func sanitizeLabelKey(key string, isPrefix bool) string {
	if len(key) == 0 {
		return key
	}
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return key
	}
	if isPrefix && key[0] >= '0' && key[0] <= '9' {
		key = "_" + key
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, key)
}

// appendSanitized appends the sanitized key to the slice
func appendSanitized(to, key []byte) []byte {
	key = bytes.TrimSpace(key)

	if len(key) == 0 {
		return to
	}

	// Add prefix underscore for digit-starting keys (both top-level and nested)
	if key[0] >= '0' && key[0] <= '9' {
		to = append(to, '_')
	}

	for _, r := range bytes.Runes(key) {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' && (r < '0' || r > '9') {
			to = append(to, jsonSpacer)
			continue
		}
		to = append(to, byte(r))
	}
	return to
}
