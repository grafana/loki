package encoder

import (
	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonstring"
)

// The names of a map whose keys may have the same name, of the v2 semantics, which a function, a method or the
// kind of its key writes. The names of a map which is not sorted are checked as they are written ( see
// appendMapKeyName ). The keys of a sorted map are sorted by their names, which puts the same names next to each
// other, and checked by OpMapKey as the entries are written in that order, when the end of the map is
// OpMapEndMayRepeatNames.

// SameMapName reports whether key, the name of an entry of a sorted map as it is encoded with a comma after it ( see
// MapItem ), is the name of the entry before it, prev, which is an error unless AllowDuplicateNamesOption.
func SameMapName(ctx *RuntimeContext, key, prev []byte) bool {
	return ctx.Option.Flag&AllowDuplicateNamesOption == 0 && string(key) == string(prev)
}

// DuplicateMapNameError returns the error of key, the name of an entry of a sorted map as it is encoded with a comma
// after it, which an entry before it has: out is the output before the name.
func DuplicateMapNameError(key, out []byte) error {
	name := jsonstring.AppendUnescaped(nil, key[1:len(key)-2])
	return &errors.TextError{Err: errors.ErrDuplicateName, Name: string(name), Out: out}
}

// stringStart returns where the string whose closing quote is at end starts: the quote before it which no odd
// number of backslashes precedes, which would escape it.
func stringStart(b []byte, end int) int {
	for i := end - 1; i >= 0; i-- {
		if b[i] != '"' {
			continue
		}
		n := 0
		for j := i - 1; j >= 0 && b[j] == '\\'; j-- {
			n++
		}
		if n%2 == 0 {
			return i
		}
	}
	return 0
}

// stringEnd returns where the string which starts at start ends: its closing quote.
func stringEnd(b []byte, start int) int {
	for i := start + 1; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return len(b) - 1
}

// memberNames returns where the names of the members of the object obj start: the object is compact and valid.
func memberNames(obj []byte) []int {
	var names []int
	depth := 0
	expectName := false
	for i := 0; i < len(obj); i++ {
		switch obj[i] {
		case '{', '[':
			depth++
			expectName = obj[i] == '{' && depth == 1
		case '}', ']':
			depth--
		case ',':
			expectName = depth == 1
		case '"':
			if expectName {
				names = append(names, i)
				expectName = false
			}
			i = stringEnd(obj, i)
		}
	}
	return names
}
