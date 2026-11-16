// Package json provides a lenient, allocation-conscious wrapper around the Go json/v2 jsontext decoder.
package json

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"sync"
)

// Be lenient: last duplicate wins, invalid UTF-8 is passed through.
var options = []jsontext.Options{jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)}

// Decoder wraps a jsontext.Decoder with helpers for reading objects, arrays and strings
// without allocating, using scratch space that is re-used between calls.
type Decoder struct {
	*jsontext.Decoder
	r   bytes.Reader // used when decoding from a byte slice
	buf []byte       // scratch space for unquoting strings
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{Decoder: jsontext.NewDecoder(r, options...)}
}

var pool = sync.Pool{New: func() any {
	d := &Decoder{}
	d.Decoder = jsontext.NewDecoder(&d.r, options...)
	return d
}}

// GetDecoder returns a pooled Decoder reading from data; give it back with Release.
func GetDecoder(data []byte) *Decoder {
	d := pool.Get().(*Decoder)
	d.r.Reset(data)
	d.Reset(&d.r, options...)
	return d
}

// Release returns a Decoder obtained from GetDecoder to the pool. It must not be used afterwards.
func (d *Decoder) Release() {
	d.r.Reset(nil)
	pool.Put(d)
}

// EachField reads an object, calling fn for each key.
// fn must consume the value, and must not use key after doing so.
func (d *Decoder) EachField(fn func(key []byte) error) error {
	if tok, err := d.ReadToken(); err != nil {
		return err
	} else if tok.Kind() != jsontext.KindBeginObject {
		return errors.New("expected object")
	}
	for d.PeekKind() != jsontext.KindEndObject {
		key, err := d.ReadString("object key must be a string")
		if err != nil {
			return err
		}
		if err := fn(key); err != nil {
			return err
		}
	}
	_, err := d.ReadToken() // consume the closing '}'
	return err
}

// EachElement reads an array, calling fn for each element, which it must consume.
func (d *Decoder) EachElement(fn func() error) error {
	if tok, err := d.ReadToken(); err != nil {
		return err
	} else if tok.Kind() != jsontext.KindBeginArray {
		return errors.New("expected array")
	}
	for d.PeekKind() != jsontext.KindEndArray {
		if err := fn(); err != nil {
			return err
		}
	}
	_, err := d.ReadToken() // consume the closing ']'
	return err
}

// ReadString reads a JSON string and returns its unquoted contents, or an error with msg if the next value isn't a string.
// The returned slice is only valid until the next call to the decoder.
func (d *Decoder) ReadString(msg string) ([]byte, error) {
	v, err := d.ReadValue()
	if err != nil {
		return nil, err
	}
	if v.Kind() != jsontext.KindString {
		return nil, errors.New(msg)
	}
	if contents := v[1 : len(v)-1]; bytes.IndexByte(contents, '\\') < 0 {
		return contents, nil
	}
	// Unquote into a buffer we re-use, to save the allocation that Token.String() would make.
	d.buf, err = jsontext.AppendUnquote(d.buf[:0], v)
	return d.buf, err
}

// ReadScalar reads a string, or the raw text of any other non-container value.
func (d *Decoder) ReadScalar() (string, error) {
	v, err := d.ReadValue()
	if err != nil {
		return "", err
	}
	switch v.Kind() {
	case jsontext.KindBeginObject, jsontext.KindBeginArray:
		return "", errors.New("expected a scalar value")
	case jsontext.KindString:
		if contents := v[1 : len(v)-1]; bytes.IndexByte(contents, '\\') < 0 {
			return string(contents), nil
		}
		d.buf, err = jsontext.AppendUnquote(d.buf[:0], v)
		return string(d.buf), err
	}
	return string(v), nil
}
