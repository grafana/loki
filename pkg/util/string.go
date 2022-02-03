package util

import (
	"bytes"
	"fmt"
	"unicode"
)

func StringRef(value string) *string {
	return &value
}

// SnakeCase converts given string `s` into `snake_case`.
func SnakeCase(s string) string {
	var buf bytes.Buffer
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 && s[i-1] != '_' {
			fmt.Fprintf(&buf, "_")
		}
		r = unicode.ToLower(r)
		fmt.Fprintf(&buf, "%c", r)
	}
	return buf.String()
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
