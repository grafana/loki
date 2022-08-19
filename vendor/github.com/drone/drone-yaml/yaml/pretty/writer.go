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
	"bytes"
	"fmt"
	"sort"
	"strconv"

	"github.com/drone/drone-yaml/yaml"
)

// TODO rename WriteTag to WriteKey
// TODO rename WriteTagValue to WriteKeyValue

// ESCAPING:
//
//   The string starts with a special character:
//   One of !#%@&*`?|>{[ or -.
//   The string starts or ends with whitespace characters.
//   The string contains : or # character sequences.
//   The string ends with a colon.
//   The value looks like a number or boolean (123, 1.23, true, false, null) but should be a string.

// Implement state pooling. See:
//     https://golang.org/src/fmt/print.go#L131

type writer interface {
	// Indent appends padding to the buffer.
	Indent()

	// IndentIncrease increases indentation.
	IndentIncrease()

	// IndentDecrease decreases indentation.
	IndentDecrease()

	// IncludeZero includes zero value values
	IncludeZero()

	// ExcludeZero excludes zero value values.
	ExcludeZero()

	// Write appends the contents of p to the buffer.
	Write(p []byte) (n int, err error)

	// WriteByte appends the contents of b to the buffer.
	WriteByte(b byte) error

	// WriteString appends the contents of s to the buffer.
	WriteString(s string) (n int, err error)

	// WriteTag appends the key to the buffer.
	WriteTag(v interface{})

	// WriteTag appends the keypair to the buffer.
	WriteTagValue(k, v interface{})
}

//
// node writer
//

type baseWriter struct {
	bytes.Buffer
	depth int
	zero  bool
}

func (w *baseWriter) Indent() {
	for i := 0; i < w.depth; i++ {
		w.WriteString("  ")
	}
}

func (w *baseWriter) IndentIncrease() {
	w.depth++
}

func (w *baseWriter) IndentDecrease() {
	w.depth--
}

func (w *baseWriter) IncludeZero() {
	w.zero = true
}

func (w *baseWriter) ExcludeZero() {
	w.zero = false
}

func (w *baseWriter) WriteTag(v interface{}) {
	w.WriteByte('\n')
	w.Indent()
	writeValue(w, v)
	w.WriteByte(':')
}

func (w *baseWriter) WriteTagValue(k, v interface{}) {
	if isZero(v) && w.zero == false {
		return
	}
	w.WriteTag(k)
	if isPrimative(v) {
		w.WriteByte(' ')
		writeValue(w, v)
	} else if isSlice(v) {
		w.WriteByte('\n')
		w.Indent()
		writeValue(w, v)
	} else {
		w.depth++
		w.WriteByte('\n')
		w.Indent()
		writeValue(w, v)
		w.depth--
	}
}

//
// sequence writer
//

type indexWriter struct {
	writer
	index int
	zero  bool
}

func (w *indexWriter) IncludeZero() {
	w.zero = true
}

func (w *indexWriter) ExcludeZero() {
	w.zero = false
}

func (w *indexWriter) WriteTag(v interface{}) {
	w.WriteByte('\n')
	if w.index == 0 {
		w.IndentDecrease()
		w.Indent()
		w.IndentIncrease()
		w.WriteByte('-')
		w.WriteByte(' ')
	} else {
		w.Indent()
	}
	writeValue(w, v)
	w.WriteByte(':')
	w.index++
}

func (w *indexWriter) WriteTagValue(k, v interface{}) {
	if isZero(v) && w.zero == false {
		return
	}
	w.WriteTag(k)
	if isPrimative(v) {
		w.WriteByte(' ')
		writeValue(w, v)
	} else if isSlice(v) {
		w.WriteByte('\n')
		w.Indent()
		writeValue(w, v)
	} else {
		w.IndentIncrease()
		w.WriteByte('\n')
		w.Indent()
		writeValue(w, v)
		w.IndentDecrease()
	}
}

//
// helper functions
//

func writeBool(w writer, v bool) {
	w.WriteString(
		strconv.FormatBool(v),
	)
}

func writeFloat(w writer, v float64) {
	w.WriteString(
		strconv.FormatFloat(v, 'g', -1, 64),
	)
}

func writeInt(w writer, v int) {
	w.WriteString(
		strconv.Itoa(v),
	)
}

func writeInt64(w writer, v int64) {
	w.WriteString(
		strconv.FormatInt(v, 10),
	)
}

func writeEncode(w writer, v string) {
	if len(v) == 0 {
		w.WriteByte('"')
		w.WriteByte('"')
		return
	}
	if isQuoted(v) {
		fmt.Fprintf(w, "%q", v)
	} else {
		w.WriteString(v)
	}
}

func writeValue(w writer, v interface{}) {
	if v == nil {
		w.WriteByte('~')
		return
	}
	switch v := v.(type) {
	case bool, int, int64, float64, string:
		writeScalar(w, v)
	case []interface{}:
		writeSequence(w, v)
	case []string:
		writeSequenceStr(w, v)
	case map[interface{}]interface{}:
		writeMapping(w, v)
	case map[string]string:
		writeMappingStr(w, v)
	case yaml.BytesSize:
		writeValue(w, v.String())
	}
}

func writeScalar(w writer, v interface{}) {
	switch v := v.(type) {
	case bool:
		writeBool(w, v)
	case int:
		writeInt(w, v)
	case int64:
		writeInt64(w, v)
	case float64:
		writeFloat(w, v)
	case string:
		writeEncode(w, v)
	}
}

func writeSequence(w writer, v []interface{}) {
	if len(v) == 0 {
		w.WriteByte('[')
		w.WriteByte(']')
		return
	}
	for i, v := range v {
		if i != 0 {
			w.WriteByte('\n')
			w.Indent()
		}
		w.WriteByte('-')
		w.WriteByte(' ')
		w.IndentIncrease()
		writeValue(w, v)
		w.IndentDecrease()
	}
}

func writeSequenceStr(w writer, v []string) {
	if len(v) == 0 {
		w.WriteByte('[')
		w.WriteByte(']')
		return
	}
	for i, v := range v {
		if i != 0 {
			w.WriteByte('\n')
			w.Indent()
		}
		w.WriteByte('-')
		w.WriteByte(' ')
		writeEncode(w, v)
	}
}

func writeMapping(w writer, v map[interface{}]interface{}) {
	if len(v) == 0 {
		w.WriteByte('{')
		w.WriteByte('}')
		return
	}
	var keys []string
	for k := range v {
		s := fmt.Sprint(k)
		keys = append(keys, s)
	}
	sort.Strings(keys)
	for i, k := range keys {
		v := v[k]
		if i != 0 {
			w.WriteByte('\n')
			w.Indent()
		}
		writeEncode(w, k)
		w.WriteByte(':')
		if v == nil || isPrimative(v) || isZero(v) {
			w.WriteByte(' ')
			writeValue(w, v)
		} else {
			slice := isSlice(v)
			if !slice {
				w.IndentIncrease()
			}
			w.WriteByte('\n')
			w.Indent()
			writeValue(w, v)
			if !slice {
				w.IndentDecrease()
			}
		}
	}
}

func writeMappingStr(w writer, v map[string]string) {
	if len(v) == 0 {
		w.WriteByte('{')
		w.WriteByte('}')
		return
	}
	var keys []string
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		v := v[k]
		if i != 0 {
			w.WriteByte('\n')
			w.Indent()
		}
		writeEncode(w, k)
		w.WriteByte(':')
		w.WriteByte(' ')
		writeEncode(w, v)
	}
}
