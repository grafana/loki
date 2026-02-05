package compute

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// RegexpMatch computes the regular expression match against a datum.
// It returns a boolean datum indicating whether the regular expression matched the haystack datum.
//
// Special cases:
//
//   - If a value in the haystack is null, the result for that value is null.
//   - If the regexp is null, the result is null.
func RegexpMatch(alloc *memory.Allocator, haystack columnar.Datum, regexp *regexp.Regexp, selection memory.Bitmap) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 {
		return nil, fmt.Errorf("haystack must be UTF-8; got %s", haystack.Kind())
	}

	_, haystackArray := haystack.(columnar.Array)

	switch {
	case haystackArray:
		return regexpMatchAS(alloc, haystack.(*columnar.UTF8), regexp, selection)
	case !haystackArray:
		return regexpMatchSS(alloc, haystack.(*columnar.UTF8Scalar), regexp)
	default:
		return nil, errors.New("unsupported haystack and regexp types")
	}
}

func regexpMatchAS(alloc *memory.Allocator, haystack *columnar.UTF8, regexp *regexp.Regexp, selection memory.Bitmap) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if regexp == nil {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	allSelected := selection.Len() == 0
	if allSelected {
		for i := range haystack.Len() {
			if haystack.IsNull(i) {
				results.AppendNull()
				continue
			}

			results.AppendValue(regexp.Match(haystack.Get(i)))
		}

		return results.Build(), nil
	}

	// Only a subset of the rows is selected, iterate over selected and append them (backfilling the results with nulls for
	// non-selected)
	prev := -1
	for curr := range selection.IterValues(true) {
		// insert nulls for all elements (prev, curr) or (prev, curr] depending on haystack.IsNull
		numNulls := curr - (prev + 1)
		currIsNull := haystack.IsNull(curr)
		if currIsNull {
			numNulls++
		}
		if numNulls > 0 {
			results.AppendNulls(numNulls)
		}
		prev = curr
		if currIsNull {
			continue
		}

		results.AppendValue(regexp.Match(haystack.Get(curr)))
	}

	// Adding trailing nulls:
	// - len=10, 0 iterated  => prev=-1 => need to append 10-(-1+1) = 10 nulls
	// - len=10, 9 iterated  => prev=8  => need to append 10-(8+1)  = 1 nulls
	// - len=10, 10 iterated => prev=9 => need to append 10-(9+1)  = 0 nulls
	trailingNulls := haystack.Len() - (prev + 1)
	results.AppendNulls(trailingNulls)

	return results.Build(), nil
}

func regexpMatchSS(_ *memory.Allocator, haystack *columnar.UTF8Scalar, regexp *regexp.Regexp) (*columnar.BoolScalar, error) {
	if regexp == nil || haystack.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	return &columnar.BoolScalar{Value: regexp.Match(haystack.Value)}, nil
}

// SubstrInsensitive computes the case insensitive match of a needle datum against haystack datum
// It returns a boolean datum indicating whether the needle was found in the haystack.
//
// Special cases:
//
//   - If a value in the haystack is null, the result for that value is null.
//   - If the needle is null, the result is null.
func SubstrInsensitive(alloc *memory.Allocator, haystack columnar.Datum, needle columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || needle.Kind() != columnar.KindUTF8 {
		return nil, fmt.Errorf("haystack and needle must both be UTF-8; got %s and %s", haystack.Kind(), needle.Kind())
	}

	if _, needleArray := needle.(columnar.Array); needleArray {
		return nil, errors.New("needle array unsupported")
	}

	_, haystackArray := haystack.(columnar.Array)

	switch {
	case haystackArray:
		return substrInsensitiveAS(alloc, haystack.(*columnar.UTF8), needle.(*columnar.UTF8Scalar), selection)
	case !haystackArray:
		return substrInsensitiveSS(alloc, haystack.(*columnar.UTF8Scalar), needle.(*columnar.UTF8Scalar))
	default:
		return nil, errors.New("unsupported haystack and needle types")
	}
}

func substrInsensitiveAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle *columnar.UTF8Scalar, selection memory.Bitmap) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if needle.IsNull() {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	needleUpper := bytes.ToUpper(needle.Value)

	allSelected := selection.Len() == 0
	if allSelected {
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

	// Only a subset of the rows is selected, iterate over selected and append them (backfilling the results with nulls for
	// non-selected)
	prev := -1
	for curr := range selection.IterValues(true) {
		// insert nulls for all elements (prev, curr) or (prev, curr] depending on haystack.IsNull
		numNulls := curr - (prev + 1)
		currIsNull := haystack.IsNull(curr)
		if currIsNull {
			numNulls++
		}
		if numNulls > 0 {
			results.AppendNulls(numNulls)
		}
		prev = curr
		if currIsNull {
			continue
		}

		haystackValueUpper := bytes.ToUpper(haystack.Get(curr))
		results.AppendValue(bytes.Contains(haystackValueUpper, needleUpper))
	}

	// Adding trailing nulls:
	// - len=10, 0 iterated  => prev=-1 => need to append 10-(-1+1) = 10 nulls
	// - len=10, 9 iterated  => prev=8  => need to append 10-(8+1)  = 1 nulls
	// - len=10, 10 iterated => prev=9 => need to append 10-(9+1)  = 0 nulls
	trailingNulls := haystack.Len() - (prev + 1)
	results.AppendNulls(trailingNulls)

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

// Substr computes the case sensitive match of a needle datum against haystack datum
// It returns a boolean datum indicating whether the needle was found in the haystack.
//
// Special cases:
//
//   - If a value in the haystack is null, the result for that value is null.
//   - If the needle is null, the result is null.
func Substr(alloc *memory.Allocator, haystack columnar.Datum, needle columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	if haystack.Kind() != columnar.KindUTF8 || needle.Kind() != columnar.KindUTF8 {
		return nil, fmt.Errorf("haystack and needle must both be UTF-8; got %s and %s", haystack.Kind(), needle.Kind())
	}

	if _, needleArray := needle.(columnar.Array); needleArray {
		return nil, errors.New("needle array unsupported")
	}

	_, haystackArray := haystack.(columnar.Array)

	switch {
	case haystackArray:
		return substrAS(alloc, haystack.(*columnar.UTF8), needle.(*columnar.UTF8Scalar), selection)
	case !haystackArray:
		return substrSS(alloc, haystack.(*columnar.UTF8Scalar), needle.(*columnar.UTF8Scalar))
	default:
		return nil, errors.New("unsupported haystack and needle types")
	}
}

func substrAS(alloc *memory.Allocator, haystack *columnar.UTF8, needle *columnar.UTF8Scalar, selection memory.Bitmap) (*columnar.Bool, error) {
	results := columnar.NewBoolBuilder(alloc)
	results.Grow(haystack.Len())

	if needle.IsNull() {
		results.AppendNulls(haystack.Len())
		return results.Build(), nil
	}

	allSelected := selection.Len() == 0
	if allSelected {
		for i := range haystack.Len() {
			if haystack.IsNull(i) {
				results.AppendNull()
				continue
			}

			results.AppendValue(bytes.Contains(haystack.Get(i), needle.Value))
		}

		return results.Build(), nil
	}

	// Only a subset of the rows is selected, iterate over selected and append them (backfilling the results with nulls for
	// non-selected)
	prev := -1
	for curr := range selection.IterValues(true) {
		// insert nulls for all elements (prev, curr) or (prev, curr] depending on haystack.IsNull
		numNulls := curr - (prev + 1)
		currIsNull := haystack.IsNull(curr)
		if currIsNull {
			numNulls++
		}
		if numNulls > 0 {
			results.AppendNulls(numNulls)
		}
		prev = curr
		if currIsNull {
			continue
		}

		results.AppendValue(bytes.Contains(haystack.Get(curr), needle.Value))
	}

	// Adding trailing nulls:
	// - len=10, 0 iterated  => prev=-1 => need to append 10-(-1+1) = 10 nulls
	// - len=10, 9 iterated  => prev=8  => need to append 10-(8+1)  = 1 nulls
	// - len=10, 10 iterated => prev=9 => need to append 10-(9+1)  = 0 nulls
	trailingNulls := haystack.Len() - (prev + 1)
	results.AppendNulls(trailingNulls)

	return results.Build(), nil
}

func substrSS(_ *memory.Allocator, haystack *columnar.UTF8Scalar, needle *columnar.UTF8Scalar) (*columnar.BoolScalar, error) {
	if needle.IsNull() || haystack.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	return &columnar.BoolScalar{Value: bytes.Contains(haystack.Value, needle.Value)}, nil
}
