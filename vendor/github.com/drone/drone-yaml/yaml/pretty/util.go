// Copyright the Drone Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pretty

import (
	"strings"

	"github.com/drone/drone-yaml/yaml"
)

func isPrimative(v interface{}) bool {
	switch v.(type) {
	case bool, string, int, int64, float64:
		return true
	case yaml.BytesSize:
		return true
	default:
		return false
	}
}

func isSlice(v interface{}) bool {
	switch v.(type) {
	case []interface{}:
		return true
	case []string:
		return true
	default:
		return false
	}
}

func isZero(v interface{}) bool {
	switch v := v.(type) {
	case bool:
		return v == false
	case string:
		return len(v) == 0
	case int:
		return v == 0
	case float64:
		return v == 0
	case []interface{}:
		return len(v) == 0
	case []string:
		return len(v) == 0
	case map[interface{}]interface{}:
		return len(v) == 0
	case map[string]string:
		return len(v) == 0
	case yaml.BytesSize:
		return int64(v) == 0
	default:
		return false
	}
}

func isEscapeCode(b rune) bool {
	switch b {
	case '\a', '\b', '\f', '\n', '\r', '\t', '\v':
		return true
	default:
		return false
	}
}

func isQuoted(s string) bool {
	// if the string is empty it should be quoted.
	if len(s) == 0 {
		return true
	}

	var r0, r1 byte
	t := strings.TrimSpace(s)

	// if the trimmed string does not match the string, it
	// has starting or tailing white space and therefore
	// needs to be quoted to preserve the whitespace
	if t != s {
		return true
	}

	if len(t) > 0 {
		r0 = t[0]
	}
	if len(t) > 1 {
		r1 = t[1]
	}

	switch r0 {
	// if the yaml starts with any of these characters
	// the string should be quoted.
	case ',', '[', ']', '{', '}', '*', '"', '\'', '%', '@', '`', '|', '>', '#':
		return true

	case '&', '!', '-', ':', '?':
		// if the yaml starts with any of these characters,
		// followed by whitespace, the string should be quoted.
		if r1 == ' ' {
			return true
		}
	}

	var prev rune
	for _, b := range s {
		switch {
		case isEscapeCode(b):
			return true
		case b == ' ' && prev == ':':
			return true
		case b == '#' && prev == ' ':
			return true
		}
		prev = b
	}

	// if the string ends in : it should be quoted otherwise
	// it is interpreted as an object.
	return strings.HasSuffix(t, ":")
}

func chunk(s string, chunkSize int) []string {
	if len(s) == 0 {
		return []string{s}
	}
	var chunks []string
	for i := 0; i < len(s); i += chunkSize {
		nn := i + chunkSize
		if nn > len(s) {
			nn = len(s)
		}
		chunks = append(chunks, s[i:nn])
	}
	return chunks
}
