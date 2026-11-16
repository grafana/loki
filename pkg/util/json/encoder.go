package json

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Encoder wraps a jsontext.Encoder and remembers the first error.
// Once an error has occurred all further writes are no-ops, so callers can
// write a whole structure and check Err once at the end.
type Encoder struct {
	enc *jsontext.Encoder
	err error
}

// Latching returns an Encoder that writes via e and latches the first error.
func Latching(e *jsontext.Encoder) *Encoder {
	return &Encoder{enc: e}
}

// Err returns the first error recorded, or nil.
func (e *Encoder) Err() error {
	return e.err
}

// MarshalJSONFrom marshals obj to the encoder.
func (e *Encoder) MarshalJSONFrom(obj json.MarshalerTo) {
	if e.err == nil {
		e.err = obj.MarshalJSONTo(e.enc)
	}
}

// Fail records err unless an earlier error has already been recorded.
func (e *Encoder) Fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// Tokens writes each token in turn.
func (e *Encoder) Tokens(toks ...jsontext.Token) {
	for _, t := range toks {
		if e.err != nil {
			return
		}
		e.err = e.enc.WriteToken(t)
	}
}

// Value writes a pre-encoded JSON value.
func (e *Encoder) Value(v jsontext.Value) {
	if e.err == nil {
		e.err = e.enc.WriteValue(v)
	}
}

// StringField writes an object member name followed by a string value.
func (e *Encoder) StringField(name, value string) {
	e.Tokens(jsontext.String(name), jsontext.String(value))
}

// Marshal writes v using the semantics of encoding/json v1.
func (e *Encoder) Marshal(v any) {
	if e.err == nil {
		e.err = json.MarshalEncode(e.enc, v, jsonv1.DefaultOptionsV1())
	}
}
