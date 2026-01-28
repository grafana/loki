package compute

import (
	"bytes"
	"errors"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func SubstrInsensitive(alloc *memory.Allocator, haystack columnar.Datum, needle columnar.Datum) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || needle.Kind() != columnar.KindUTF8 {
		return nil, errors.New("haystack and needle must be UTF-8")
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

// SubstrInsensitiveAS computes the substring insensitive match of a string within an array of strings.
// It returns a boolean array indicating whether the substring was found in the haystack.
//
// Special cases:
//
//   - If the haystack is null, the result is an empty boolean array.
//   - If the needle is null, the result is a boolean array with all values set to false.
func SubstrInsensitiveAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle *columnar.UTF8Scalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	needleLower := bytes.ToUpper(needle.Value)

	for i := range haystack.Len() {
		haystackValue := haystack.Get(i)
		haystackValueUpper := bytes.ToUpper(haystackValue)
		results.AppendValue(bytes.Contains(haystackValueUpper, needleLower))
	}
	return results.Build(), nil
}

func SubstrInsensitiveSS(alloc *memory.Allocator, haystack *columnar.UTF8Scalar, needle *columnar.UTF8Scalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(1)

	needleLower := bytes.ToUpper(needle.Value)

	haystackValue := haystack.Value
	haystackValueLower := bytes.ToUpper(haystackValue)
	results.AppendValue(bytes.Contains(haystackValueLower, needleLower))
	return results.Build(), nil
}
