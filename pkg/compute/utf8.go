package compute

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// SubstrInsensitiveAS computes the substring insensitive match of a string within an array of strings.
// It returns a boolean array indicating whether the substring was found in the haystack.
//
// Special cases:
//
//   - If the haystack is null, the result is an empty boolean array.
//   - If the needle is null, the result is a boolean array with all values set to false.
func SubstrInsensitive(alloc *memory.Allocator, haystack columnar.Datum, needle columnar.Datum) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || needle.Kind() != columnar.KindUTF8 {
		return nil, fmt.Errorf("haystack and needle must both be UTF-8; got %s and %s", haystack.Kind(), needle.Kind())
	}

	_, haystackArray := haystack.(columnar.Array)
	_, needleArray := needle.(columnar.Array)

	switch {
	case haystackArray && needleArray:
		return nil, errors.New("unsupported haystack and needle types")
	case haystackArray && !needleArray:
		return SubstrInsensitiveAS(alloc, haystack.(*columnar.UTF8), needle.(*columnar.UTF8Scalar))
	case !haystackArray && needleArray:
		return nil, errors.New("unsupported haystack and needle types")
	case !haystackArray && !needleArray:
		return SubstrInsensitiveSS(alloc, haystack.(*columnar.UTF8Scalar), needle.(*columnar.UTF8Scalar))
	default:
		return nil, errors.New("unsupported haystack and needle types")
	}
}

func SubstrInsensitiveAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle *columnar.UTF8Scalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if needle.IsNull() {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	biggestHaystackValue := 0
	for i := range haystack.Len() {
		haystackValue := haystack.Get(i)
		biggestHaystackValue = max(biggestHaystackValue, len(haystackValue))
	}

	needleBuffer := memory.MakeBuffer[byte](alloc, len(needle.Value))
	needleUpper := toUpper(needle.Value, needleBuffer.Data()[:len(needle.Value)])

	workBuffer := memory.MakeBuffer[byte](alloc, biggestHaystackValue)
	workBytes := workBuffer.Data()

	for i := range haystack.Len() {
		if haystack.IsNull(i) {
			results.AppendNull()
			continue
		}

		haystackValue := haystack.Get(i)
		haystackValueUpper := toUpper(haystackValue, workBytes[:len(haystackValue)])
		results.AppendValue(bytes.Contains(haystackValueUpper, needleUpper))
	}
	return results.Build(), nil
}

func SubstrInsensitiveSS(alloc *memory.Allocator, haystack *columnar.UTF8Scalar, needle *columnar.UTF8Scalar) (*columnar.BoolScalar, error) {
	if needle.IsNull() || haystack.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	needleBuffer := memory.MakeBuffer[byte](alloc, len(needle.Value))
	needleUpper := toUpper(needle.Value, needleBuffer.Data()[:len(needle.Value)])

	haystackValue := haystack.Value
	haystackBuffer := memory.MakeBuffer[byte](alloc, len(haystackValue))
	haystackValueUpper := toUpper(haystackValue, haystackBuffer.Data()[:len(haystackValue)])
	return &columnar.BoolScalar{Value: bytes.Contains(haystackValueUpper, needleUpper)}, nil
}

// toUpper is an optimized version of bytes.ToUpper that uses a fast path for ASCII-only strings.
// For ASCII strings, it subtracts 32 from lowercase letters ('a'-'z') to convert to uppercase.
// For strings containing non-ASCII characters, it falls back to bytes.ToUpper.
// The result is written to the provided result buffer. This function panics if the buffer length is too small.
func toUpper(b []byte, result []byte) []byte {
	if len(b) == 0 {
		return b
	}
	if len(b) != len(result) {
		panic("buffer length mismatch")
	}

	for i := 0; i < len(b); i++ {
		c := b[i]
		// If we encounter a non-ASCII byte, fall back to standard library
		if c >= utf8.RuneSelf {
			return bytes.ToUpper(b)
		}
		// Fast ASCII path: subtract 32 from 'a'-'z' range
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A' // subtract 32
		}
		result[i] = c
	}
	return result
}
