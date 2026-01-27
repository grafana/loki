package compute

import (
	"bytes"

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
func SubstrInsensitiveAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle columnar.UTF8Scalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	needleLower := bytes.ToLower(needle.Value)

	for i := range haystack.Len() {
		haystackValue := haystack.Get(i)
		haystackValueLower := bytes.ToLower(haystackValue)
		results.AppendValue(bytes.Contains(haystackValueLower, needleLower))
	}
	return results.Build(), nil
}
