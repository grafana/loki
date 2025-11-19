// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package json

import (
	"bytes"
	"encoding/json"
	"strings"
)

type jwriter struct {
	buf *bytes.Buffer
	err error
}

func newJWriter() *jwriter {
	buf := make([]byte, 0, sensibleBufferSize)

	return &jwriter{buf: bytes.NewBuffer(buf)}
}

func (w *jwriter) Reset() {
	w.buf.Reset()
	w.err = nil
}

func (w *jwriter) RawString(s string) {
	if w.err != nil {
		return
	}
	w.buf.WriteString(s)
}

func (w *jwriter) Raw(b []byte, err error) {
	if w.err != nil {
		return
	}
	if err != nil {
		w.err = err
		return
	}

	_, _ = w.buf.Write(b)
}

func (w *jwriter) RawByte(c byte) {
	if w.err != nil {
		return
	}
	w.buf.WriteByte(c)
}

var quoteReplacer = strings.NewReplacer(`"`, `\"`, `\`, `\\`)

func (w *jwriter) String(s string) {
	if w.err != nil {
		return
	}
	// escape quotes and \
	s = quoteReplacer.Replace(s)

	_ = w.buf.WriteByte('"')
	json.HTMLEscape(w.buf, []byte(s))
	_ = w.buf.WriteByte('"')
}

// BuildBytes returns a clone of the internal buffer.
func (w *jwriter) BuildBytes() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}

	return bytes.Clone(w.buf.Bytes()), nil
}
