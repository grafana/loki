// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Tag resolution for YAML scalars.
// Determines implicit types (int, float, bool, null, timestamp) from untagged
// scalar values.

package libyaml

import (
	"encoding/base64"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type resolveMapItem struct {
	value any
	tag   string
}

var (
	resolveTable = make([]byte, 256)
	resolveMap   = make(map[string]resolveMapItem)
)

// negativeZero represents -0.0 for YAML encoding/decoding
// this is needed because Go constants cannot express -0.0
// https://staticcheck.dev/docs/checks/#SA4026
var negativeZero = math.Copysign(0.0, -1.0)

func init() {
	t := resolveTable
	t[int('+')] = 'S' // Sign
	t[int('-')] = 'S'
	for _, c := range "0123456789" {
		t[int(c)] = 'D' // Digit
	}
	for _, c := range "yYnNtTfFoO~<" { // < for merge key <<
		t[int(c)] = 'M' // In map
	}
	t[int('.')] = '.' // Float (potentially in map)

	resolveMapList := []struct {
		v   any
		tag string
		l   []string
	}{
		{true, boolTag, []string{"true", "True", "TRUE"}},
		{false, boolTag, []string{"false", "False", "FALSE"}},
		{nil, nullTag, []string{"", "~", "null", "Null", "NULL"}},
		{math.NaN(), floatTag, []string{".nan", ".NaN", ".NAN"}},
		{math.Inf(+1), floatTag, []string{".inf", ".Inf", ".INF"}},
		{math.Inf(+1), floatTag, []string{"+.inf", "+.Inf", "+.INF"}},
		{math.Inf(-1), floatTag, []string{"-.inf", "-.Inf", "-.INF"}},
		{negativeZero, floatTag, []string{"-0", "-0.0"}},
		{"<<", mergeTag, []string{"<<"}},
	}

	m := resolveMap
	for _, item := range resolveMapList {
		for _, s := range item.l {
			m[s] = resolveMapItem{item.v, item.tag}
		}
	}
}

func resolvableTag(tag string) bool {
	switch tag {
	case "", strTag, boolTag, intTag, floatTag, nullTag, timestampTag:
		return true
	}
	return false
}

var yamlStyleFloat = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)

func resolve(tag string, in string) (rtag string, out any) {
	tag = shortTag(tag)
	if !resolvableTag(tag) {
		return tag, in
	}

	defer func() {
		switch tag {
		case "", rtag, strTag, binaryTag:
			return
		case floatTag:
			if rtag == intTag {
				switch v := out.(type) {
				case int64:
					rtag = floatTag
					out = float64(v)
					return
				case int:
					rtag = floatTag
					out = float64(v)
					return
				}
			}
		}
		failf("cannot construct %s `%s` as a %s", shortTag(rtag), in, shortTag(tag))
	}()

	// Any data is accepted as a !!str or !!binary.
	// Otherwise, the prefix is enough of a hint about what it might be.
	hint := byte('N')
	if in != "" {
		hint = resolveTable[in[0]]
	}
	if hint != 0 && tag != strTag && tag != binaryTag {
		// Handle things we can lookup in a map.
		if item, ok := resolveMap[in]; ok {
			return item.tag, item.value
		}

		// Base 60 floats are a bad idea, were dropped in YAML 1.2, and
		// are purposefully unsupported here. They're still quoted on
		// the way out for compatibility with other parser, though.

		switch hint {
		case 'M':
			// We've already checked the map above.

		case '.':
			// Not in the map, so maybe a normal float.
			floatv, err := strconv.ParseFloat(in, 64)
			if err == nil {
				return floatTag, floatv
			}

		case 'D', 'S':
			// Int, float, or timestamp.
			// Only try values as a timestamp if the value is unquoted or there's an explicit
			// !!timestamp tag.
			if tag == "" || tag == timestampTag {
				t, ok := parseTimestamp(in)
				if ok {
					return timestampTag, t
				}
			}

			plain := strings.ReplaceAll(in, "_", "")
			intv, err := strconv.ParseInt(plain, 0, 64)
			if err == nil {
				if intv == int64(int(intv)) {
					return intTag, int(intv)
				} else {
					return intTag, intv
				}
			}
			uintv, err := strconv.ParseUint(plain, 0, 64)
			if err == nil {
				return intTag, uintv
			}
			if yamlStyleFloat.MatchString(plain) {
				floatv, err := strconv.ParseFloat(plain, 64)
				if err == nil {
					return floatTag, floatv
				}
			}
		default:
			panic("internal error: missing handler for resolver table: " + string(rune(hint)) + " (with " + in + ")")
		}
	}
	return strTag, in
}

// encodeBase64 encodes s as base64 that is broken up into multiple lines
// as appropriate for the resulting length.
func encodeBase64(s string) string {
	const lineLen = 70
	encLen := base64.StdEncoding.EncodedLen(len(s))
	lines := encLen/lineLen + 1
	buf := make([]byte, encLen*2+lines)
	in := buf[0:encLen]
	out := buf[encLen:]
	base64.StdEncoding.Encode(in, []byte(s))
	k := 0
	for i := 0; i < len(in); i += lineLen {
		j := i + lineLen
		if j > len(in) {
			j = len(in)
		}
		k += copy(out[k:], in[i:j])
		if lines > 1 {
			out[k] = '\n'
			k++
		}
	}
	return string(out[:k])
}

// This is a subset of the formats allowed by the regular expression
// defined at http://yaml.org/type/timestamp.html.
var allowedTimestampFormats = []string{
	"2006-1-2T15:4:5.999999999Z07:00", // RCF3339Nano with short date fields.
	"2006-1-2t15:4:5.999999999Z07:00", // RFC3339Nano with short date fields and lower-case "t".
	"2006-1-2 15:4:5.999999999",       // space separated with no time zone
	"2006-1-2",                        // date only
	// Notable exception: time.Parse cannot handle: "2001-12-14 21:59:43.10 -5"
	// from the set of examples.
}

// parseTimestamp parses s as a timestamp string and
// returns the timestamp and reports whether it succeeded.
// Timestamp formats are defined at http://yaml.org/type/timestamp.html
func parseTimestamp(s string) (time.Time, bool) {
	// TODO write code to check all the formats supported by
	// http://yaml.org/type/timestamp.html instead of using time.Parse.

	// Quick check: all date formats start with YYYY-.
	i := 0
	for ; i < len(s); i++ {
		if c := s[i]; c < '0' || c > '9' {
			break
		}
	}
	if i != 4 || i == len(s) || s[i] != '-' {
		return time.Time{}, false
	}
	for _, format := range allowedTimestampFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
