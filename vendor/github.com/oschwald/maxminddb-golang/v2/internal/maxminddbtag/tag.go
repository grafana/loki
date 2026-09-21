// Package maxminddbtag parses maxminddb struct tag values.
package maxminddbtag

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options is the parsed form of a maxminddb struct tag value.
type Options struct {
	Name       string
	MaxSize    uint64
	HasName    bool
	HasMaxSize bool
	Ignored    bool
}

// Parse parses a maxminddb struct tag value. Its comma-separated name and
// option grammar follows encoding/json/v2. Unknown options are ignored so
// future versions can add options without breaking older decoders.
//
//nolint:nestif // Quoted names and ordinary names share the initial token dispatch.
func Parse(tag string) (Options, error) {
	var out Options
	if !utf8.ValidString(tag) {
		return out, errors.New("must be valid UTF-8")
	}
	if tag == "-" {
		out.Ignored = true
		return out, nil
	}

	quotedName := false
	if tag != "" && tag[0] != ',' {
		if tag[0] == '\'' {
			name, n, err := consumeOption(tag)
			if err != nil {
				return out, err
			}
			out.Name, out.HasName = name, true
			tag = tag[n:]
			quotedName = true
		} else {
			n := strings.IndexByte(tag, ',')
			if n < 0 {
				n = len(tag)
			}
			out.Name, out.HasName = tag[:n], true
			tag = tag[n:]
		}
	}
	if out.HasName && out.Name == "-" && !quotedName {
		return out, fmt.Errorf(
			"name %q must be quoted as %q when options are present",
			out.Name,
			"'-'",
		)
	}

	seen := make(map[string]bool)
	for tag != "" {
		if tag[0] != ',' {
			return out, fmt.Errorf(
				"invalid character %q before next option (expecting ',')",
				tag[0],
			)
		}
		tag = tag[1:]
		if tag == "" {
			return out, errors.New("invalid trailing ',' character")
		}

		option, n, err := consumeOption(tag)
		if err != nil {
			return out, err
		}
		rawOption := tag[:n]
		tag = tag[n:]
		if rawOption[0] == '\'' && strings.TrimFunc(option, isLetterOrDigit) == "" {
			return out, fmt.Errorf(
				"unnecessarily quoted appearance of %q option; specify %q instead",
				rawOption,
				option,
			)
		}
		if seen[option] {
			return out, fmt.Errorf("duplicate appearance of %q option", rawOption)
		}
		seen[option] = true

		switch option {
		case "maxsize":
			if !strings.HasPrefix(tag, ":") {
				return out, fmt.Errorf(
					"missing value for %q option; specify %q instead",
					"maxsize",
					"maxsize:N",
				)
			}
			tag = tag[1:]
			n = strings.IndexByte(tag, ',')
			if n < 0 {
				n = len(tag)
			}
			value := tag[:n]
			if value == "" {
				return out, fmt.Errorf("missing value for %q option", "maxsize")
			}
			maximum, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return out, fmt.Errorf("invalid value %q for %q option", value, "maxsize")
			}
			out.MaxSize, out.HasMaxSize = maximum, true
			tag = tag[n:]
		default:
			normalized := strings.ReplaceAll(strings.ToLower(option), "_", "")
			if normalized == "maxsize" {
				return out, fmt.Errorf(
					"invalid appearance of %q option; specify %q instead",
					option,
					"maxsize",
				)
			}
			if strings.HasPrefix(tag, ":") {
				tag = tag[1:]
				n, err := consumeUnknownOptionValue(tag)
				if err != nil {
					return out, fmt.Errorf("invalid value for %q option: %w", option, err)
				}
				tag = tag[n:]
			}
		}
	}
	return out, nil
}

func consumeUnknownOptionValue(in string) (int, error) {
	if in == "" {
		return 0, errors.New("missing value")
	}
	if in[0] >= '0' && in[0] <= '9' {
		n := len(in) - len(strings.TrimLeftFunc(in, unicode.IsNumber))
		return n, nil
	}
	_, n, err := consumeOption(in)
	return n, err
}

// consumeOption consumes a Go identifier or a single-quoted string. This is
// the option token grammar used by encoding/json/v2 struct tags.
func consumeOption(in string) (string, int, error) {
	i := strings.IndexByte(in, ',')
	if i < 0 {
		i = len(in)
	}

	switch r, _ := utf8.DecodeRuneInString(in); {
	case r == '_' || unicode.IsLetter(r):
		n := len(in) - len(strings.TrimLeftFunc(in, isLetterOrDigit))
		return in[:n], n, nil
	case r == '\'':
		var escaped bool
		quoted := []byte{'"'}
		n := 1
		for len(in) > n {
			r, runeLen := utf8.DecodeRuneInString(in[n:])
			switch {
			case escaped:
				if r == '\'' {
					quoted = quoted[:len(quoted)-1]
				}
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				quoted = append(quoted, '\\')
			case r == '\'':
				quoted = append(quoted, '"')
				n++
				value, err := strconv.Unquote(string(quoted))
				if err != nil {
					return in[:i], i, fmt.Errorf("invalid single-quoted string: %s", in[:n])
				}
				return value, n, nil
			default:
				// Preserve the rune below.
			}
			quoted = append(quoted, in[n:n+runeLen]...)
			n += runeLen
		}
		return in[:i], i, errors.New("single-quoted string not terminated")
	case len(in) == 0:
		return "", 0, io.ErrUnexpectedEOF
	default:
		return in[:i], i, fmt.Errorf(
			"invalid character %q at start of option (expecting Unicode letter or single quote)",
			r,
		)
	}
}

func isLetterOrDigit(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}
