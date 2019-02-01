/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framer

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"
)

func TestRead(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x04,
		0x01, 0x02, 0x03, 0x04,
		0x00, 0x00, 0x00, 0x03,
		0x05, 0x06, 0x07,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01,
		0x08,
	}
	b := bytes.NewBuffer(data)
	r := NewLengthDelimitedFrameReader(ioutil.NopCloser(b))
	buf := make([]byte, 1)
	if n, err := r.Read(buf); err != io.ErrShortBuffer && n != 1 && bytes.Equal(buf, []byte{0x01}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	if n, err := r.Read(buf); err != io.ErrShortBuffer && n != 1 && bytes.Equal(buf, []byte{0x02}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read the remaining frame
	buf = make([]byte, 2)
	if n, err := r.Read(buf); err != nil && n != 2 && bytes.Equal(buf, []byte{0x03, 0x04}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read with buffer equal to frame
	buf = make([]byte, 3)
	if n, err := r.Read(buf); err != nil && n != 3 && bytes.Equal(buf, []byte{0x05, 0x06, 0x07}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read empty frame
	buf = make([]byte, 3)
	if n, err := r.Read(buf); err != nil && n != 0 && bytes.Equal(buf, []byte{}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read with larger buffer than frame
	buf = make([]byte, 3)
	if n, err := r.Read(buf); err != nil && n != 1 && bytes.Equal(buf, []byte{0x08}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read EOF
	if n, err := r.Read(buf); err != io.EOF && n != 0 {
		t.Fatalf("unexpected: %v %d", err, n)
	}
}

func TestReadLarge(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x04,
		0x01, 0x02, 0x03, 0x04,
		0x00, 0x00, 0x00, 0x03,
		0x05, 0x06, 0x07,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01,
		0x08,
	}
	b := bytes.NewBuffer(data)
	r := NewLengthDelimitedFrameReader(ioutil.NopCloser(b))
	buf := make([]byte, 40)
	if n, err := r.Read(buf); err != nil && n != 4 && bytes.Equal(buf, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil && n != 3 && bytes.Equal(buf, []byte{0x05, 0x06, 0x7}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil && n != 0 && bytes.Equal(buf, []byte{}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil && n != 1 && bytes.Equal(buf, []byte{0x08}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read EOF
	if n, err := r.Read(buf); err != io.EOF && n != 0 {
		t.Fatalf("unexpected: %v %d", err, n)
	}
}
func TestReadInvalidFrame(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x04,
		0x01, 0x02,
	}
	b := bytes.NewBuffer(data)
	r := NewLengthDelimitedFrameReader(ioutil.NopCloser(b))
	buf := make([]byte, 1)
	if n, err := r.Read(buf); err != io.ErrShortBuffer && n != 1 && bytes.Equal(buf, []byte{0x01}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read the remaining frame
	buf = make([]byte, 3)
	if n, err := r.Read(buf); err != io.ErrUnexpectedEOF && n != 1 && bytes.Equal(buf, []byte{0x02}) {
		t.Fatalf("unexpected: %v %d %v", err, n, buf)
	}
	// read EOF
	if n, err := r.Read(buf); err != io.EOF && n != 0 {
		t.Fatalf("unexpected: %v %d", err, n)
	}
}

func TestJSONFrameReader(t *testing.T) {
	b := bytes.NewBufferString("{\"test\":true}\n1\n[\"a\"]")
	r := NewJSONFramedReader(ioutil.NopCloser(b))
	buf := make([]byte, 20)
	if n, err := r.Read(buf); err != nil || n != 13 || string(buf[:n]) != `{"test":true}` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil || n != 1 || string(buf[:n]) != `1` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil || n != 5 || string(buf[:n]) != `["a"]` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != io.EOF || n != 0 {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
}

func TestJSONFrameReaderShortBuffer(t *testing.T) {
	b := bytes.NewBufferString("{\"test\":true}\n1\n[\"a\"]")
	r := NewJSONFramedReader(ioutil.NopCloser(b))
	buf := make([]byte, 3)

	if n, err := r.Read(buf); err != io.ErrShortBuffer || n != 3 || string(buf[:n]) != `{"t` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != io.ErrShortBuffer || n != 3 || string(buf[:n]) != `est` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != io.ErrShortBuffer || n != 3 || string(buf[:n]) != `":t` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != io.ErrShortBuffer || n != 3 || string(buf[:n]) != `rue` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil || n != 1 || string(buf[:n]) != `}` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}

	if n, err := r.Read(buf); err != nil || n != 1 || string(buf[:n]) != `1` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}

	if n, err := r.Read(buf); err != io.ErrShortBuffer || n != 3 || string(buf[:n]) != `["a` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
	if n, err := r.Read(buf); err != nil || n != 2 || string(buf[:n]) != `"]` {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}

	if n, err := r.Read(buf); err != io.EOF || n != 0 {
		t.Fatalf("unexpected: %v %d %q", err, n, buf)
	}
}
