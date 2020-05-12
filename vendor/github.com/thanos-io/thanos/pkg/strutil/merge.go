// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package strutil

import (
	"sort"
	"strings"
)

// MergeSlices merges a set of sorted string slices into a single ones
// while removing all duplicates.
func MergeSlices(a ...[]string) []string {
	if len(a) == 0 {
		return nil
	}
	if len(a) == 1 {
		return a[0]
	}
	l := len(a) / 2
	return mergeTwoStringSlices(MergeSlices(a[:l]...), MergeSlices(a[l:]...))
}

// MergeUnsortedSlices behaves like StringSlices but input slices are validated
// for sortedness and are sorted if they are not ordered yet.
func MergeUnsortedSlices(a ...[]string) []string {
	for _, s := range a {
		if !sort.StringsAreSorted(s) {
			sort.Strings(s)
		}
	}
	return MergeSlices(a...)
}

func mergeTwoStringSlices(a, b []string) []string {
	maxl := len(a)
	if len(b) > len(a) {
		maxl = len(b)
	}
	res := make([]string, 0, maxl*10/9)

	for len(a) > 0 && len(b) > 0 {
		d := strings.Compare(a[0], b[0])

		if d == 0 {
			res = append(res, a[0])
			a, b = a[1:], b[1:]
		} else if d < 0 {
			res = append(res, a[0])
			a = a[1:]
		} else if d > 0 {
			res = append(res, b[0])
			b = b[1:]
		}
	}
	// Append all remaining elements.
	res = append(res, a...)
	res = append(res, b...)
	return res
}
