package util //nolint:revive

import (
	"sort"
)

func StringRef(value string) *string {
	return &value
}

// StringsContain returns true if the search value is within the list of input values.
func StringsContain(values []string, search string) bool {
	for _, v := range values {
		if search == v {
			return true
		}
	}

	return false
}

// UniqueStrings keeps a slice of unique strings.
type UniqueStrings struct {
	values map[string]struct{}
	result []string
}

// NewUniqueStrings returns a UniqueStrings instance with a pre-allocated result buffer.
func NewUniqueStrings(sizeHint int) UniqueStrings {
	return UniqueStrings{result: make([]string, 0, sizeHint)}
}

// Add adds a new string, dropping duplicates.
func (us *UniqueStrings) Add(strings ...string) {
	for _, s := range strings {
		if _, ok := us.values[s]; ok {
			continue
		}
		if us.values == nil {
			us.values = map[string]struct{}{}
		}
		us.values[s] = struct{}{}
		us.result = append(us.result, s)
	}
}

// Strings returns the sorted sliced of unique strings.
func (us UniqueStrings) Strings() []string {
	sort.Strings(us.result)
	return us.result
}
