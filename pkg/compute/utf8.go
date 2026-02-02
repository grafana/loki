package compute

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// RegexpMatch computes the regular expression match of a regular expression within an array of strings.
// It returns a boolean array indicating whether the regular expression matched the haystack value.
//
// Special cases:
//
//   - If a value in the haystack is null, the result for that value is null.
//   - If the regexp is null, the result is null.
func RegexpMatch(alloc *memory.Allocator, haystack columnar.Datum, regexp columnar.Datum) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || regexp.Kind() != columnar.KindRegexp {
		return nil, fmt.Errorf("haystack and regexp must both be UTF-8; got %s and %s", haystack.Kind(), regexp.Kind())
	}

	if _, ok := regexp.(*columnar.RegexpScalar); !ok {
		return nil, fmt.Errorf("regexp must be a RegexpScalar; got %T", regexp)
	}

	_, haystackArray := haystack.(columnar.Array)

	switch {
	case haystackArray:
		return regexpMatchAS(alloc, haystack.(*columnar.UTF8), regexp.(*columnar.RegexpScalar))
	case !haystackArray:
		return regexpMatchSS(alloc, haystack.(*columnar.UTF8Scalar), regexp.(*columnar.RegexpScalar))
	default:
		return nil, errors.New("unsupported haystack and regexp types")
	}
}

func regexpMatchAS(alloc *memory.Allocator, haystack *columnar.UTF8, regexp *columnar.RegexpScalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if regexp.IsNull() {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	for i := range haystack.Len() {
		if haystack.IsNull(i) {
			results.AppendNull()
			continue
		}

		results.AppendValue(regexp.Value.Match(haystack.Get(i)))
	}
	return results.Build(), nil
}

func regexpMatchSS(_ *memory.Allocator, haystack *columnar.UTF8Scalar, regexp *columnar.RegexpScalar) (*columnar.BoolScalar, error) {
	if regexp.IsNull() || haystack.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	return &columnar.BoolScalar{Value: regexp.Value.Match(haystack.Value)}, nil
}

// SubstrInsensitive computes the case insensitive substring match of a string within an array of strings.
// It returns a boolean array indicating whether the substring was found in the haystack.
//
// Special cases:
//
//   - If a value in the haystack is null, the result for that value is null.
//   - If the needle is null, the result is null.
func SubstrInsensitive(alloc *memory.Allocator, haystack columnar.Datum, needle columnar.Datum) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || needle.Kind() != columnar.KindUTF8 {
		return nil, fmt.Errorf("haystack and needle must both be UTF-8; got %s and %s", haystack.Kind(), needle.Kind())
	}

	if _, needleArray := needle.(columnar.Array); needleArray {
		return nil, errors.New("needle array unsupported")
	}

	_, haystackArray := haystack.(columnar.Array)

	switch {
	case haystackArray:
		return substrInsensitiveAS(alloc, haystack.(*columnar.UTF8), needle.(*columnar.UTF8Scalar))
	case !haystackArray:
		return substrInsensitiveSS(alloc, haystack.(*columnar.UTF8Scalar), needle.(*columnar.UTF8Scalar))
	default:
		return nil, errors.New("unsupported haystack and needle types")
	}
}

func substrInsensitiveAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle *columnar.UTF8Scalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if needle.IsNull() {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	needleUpper := bytes.ToUpper(needle.Value)

	for i := range haystack.Len() {
		if haystack.IsNull(i) {
			results.AppendNull()
			continue
		}

		haystackValueUpper := bytes.ToUpper(haystack.Get(i))
		results.AppendValue(bytes.Contains(haystackValueUpper, needleUpper))
	}
	return results.Build(), nil
}

func substrInsensitiveSS(_ *memory.Allocator, haystack *columnar.UTF8Scalar, needle *columnar.UTF8Scalar) (*columnar.BoolScalar, error) {
	if needle.IsNull() || haystack.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	needleUpper := bytes.ToUpper(needle.Value)

	haystackValueUpper := bytes.ToUpper(haystack.Value)
	return &columnar.BoolScalar{Value: bytes.Contains(haystackValueUpper, needleUpper)}, nil
}

// Substr computes the case sensitive substring match of a string within an array of strings.
// It returns a boolean array indicating whether the substring was found in the haystack.
//
// Special cases:
//
//   - If a value in the haystack is null, the result for that value is null.
//   - If the needle is null, the result is null.
func Substr(alloc *memory.Allocator, haystack columnar.Datum, needle columnar.Datum) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || needle.Kind() != columnar.KindUTF8 {
		return nil, fmt.Errorf("haystack and needle must both be UTF-8; got %s and %s", haystack.Kind(), needle.Kind())
	}

	if _, needleArray := needle.(columnar.Array); needleArray {
		return nil, errors.New("needle array unsupported")
	}

	_, haystackArray := haystack.(columnar.Array)

	switch {
	case haystackArray:
		return substrAS(alloc, haystack.(*columnar.UTF8), needle.(*columnar.UTF8Scalar))
	case !haystackArray:
		return substrSS(alloc, haystack.(*columnar.UTF8Scalar), needle.(*columnar.UTF8Scalar))
	default:
		return nil, errors.New("unsupported haystack and needle types")
	}
}

func substrAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle *columnar.UTF8Scalar) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if needle.IsNull() {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	for i := range haystack.Len() {
		if haystack.IsNull(i) {
			results.AppendNull()
			continue
		}

		results.AppendValue(bytes.Contains(haystack.Get(i), needle.Value))
	}
	return results.Build(), nil
}

func substrSS(_ *memory.Allocator, haystack *columnar.UTF8Scalar, needle *columnar.UTF8Scalar) (*columnar.BoolScalar, error) {
	if needle.IsNull() || haystack.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	return &columnar.BoolScalar{Value: bytes.Contains(haystack.Value, needle.Value)}, nil
}
