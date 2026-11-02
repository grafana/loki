package runtime

import (
	"reflect"
	"strings"
	"unicode"
)

func getTag(field reflect.StructField) string {
	return field.Tag.Get("json")
}

func IsIgnoredStructField(field reflect.StructField) bool {
	if field.PkgPath != "" {
		if field.Anonymous {
			t := field.Type
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			if t.Kind() != reflect.Struct {
				return true
			}
		} else {
			// private field
			return true
		}
	}
	tag := getTag(field)
	return tag == "-"
}

type StructTag struct {
	Key         string
	IsTaggedKey bool
	IsOmitEmpty bool
	// IsOmitZero is whether the field has the omitzero option, which encoding/json has from Go 1.24: it is
	// false with an older Go, whose encoding/json ignores the option.
	IsOmitZero bool
	IsString   bool
	Field      reflect.StructField
	// QuotedKey is Key as a JSON string, if Key may need an escape: a name of the v2 semantics may have any
	// character.
	QuotedKey string
}

type StructTags []*StructTag

func (t StructTags) ExistsKey(key string) bool {
	for _, tt := range t {
		if tt.Key == key {
			return true
		}
	}
	return false
}

func isValidTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
			// Backslash and quote chars are reserved, but
			// otherwise any punctuation chars are allowed
			// in a tag name.
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

func StructTagFromField(field reflect.StructField) *StructTag {
	keyName := field.Name
	tag := getTag(field)
	st := &StructTag{Field: field}
	opts := strings.Split(tag, ",")
	if len(opts) > 0 {
		if opts[0] != "" && isValidTag(opts[0]) {
			keyName = opts[0]
			st.IsTaggedKey = true
		}
	}
	st.Key = keyName
	if len(opts) > 1 {
		for _, opt := range opts[1:] {
			switch opt {
			case "omitempty":
				st.IsOmitEmpty = true
			case "omitzero":
				st.IsOmitZero = omitZeroOption
			case "string":
				st.IsString = true
			}
		}
	}
	return st
}

// NamedField is a field of a struct, or of a struct embedded in it whose fields are promoted, which has a key in the
// JSON object.
type NamedField interface {
	// Depth is how deep the field is in the structs embedded in the struct: 0 for a field of the struct itself.
	Depth() int
	// IsTaggedKey reports whether the key is the name in the field's tag.
	IsTaggedKey() bool
}

// DominantField returns which one of fields, the fields of a struct which have the same key, is written and read
// with the key, as encoding/json chooses it: the least deep one, or, if more than one are at that depth, the only
// one of them with the key in its tag. It returns -1 when none is.
func DominantField[F NamedField](fields []F) int {
	depth := fields[0].Depth()
	for _, f := range fields[1:] {
		depth = min(depth, f.Depth())
	}
	dominant := -1
	var least, tagged int
	for i, f := range fields {
		if f.Depth() != depth {
			continue
		}
		least++
		if f.IsTaggedKey() {
			tagged++
			dominant = i
		} else if least == 1 {
			dominant = i
		}
	}
	if least > 1 && tagged != 1 {
		return -1
	}
	return dominant
}
