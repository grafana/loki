//go:build go1.27 && goexperiment.jsonv2

package decoder

import (
	"bytes"
	"encoding/json"
	stdjsontext "encoding/json/jsontext"
	stdjson "encoding/json/v2"
	"reflect"
	"sync"

	"github.com/goccy/go-json/internal/errors"
)

// stdUnmarshalerFromType is the interface of UnmarshalJSONFrom with the Decoder of encoding/json/jsontext, which
// encoding/json calls where it is built on encoding/json/v2.
var stdUnmarshalerFromType = reflect.TypeFor[stdjson.UnmarshalerFrom]()

// stdFromDecoder is a decoder of encoding/json/jsontext which an UnmarshalJSONFrom method reads from, and the reader
// of its input.
type stdFromDecoder struct {
	r   bytes.Reader
	dec *stdjsontext.Decoder
}

var stdFromDecoders = sync.Pool{New: func() any {
	d := new(stdFromDecoder)
	d.dec = stdjsontext.NewDecoder(&d.r)
	return d
}}

// The options of the decoder which UnmarshalJSONFrom of encoding/json/jsontext reads from, as fromOptions: the ones
// of encoding/json, its DefaultOptionsV1, and with RejectUnknownMembers for DisallowUnknownFieldsOption. UseNumberOption
// has none: encoding/json gives its methods UseNumber by an option which it doesn't export ( see
// issues/decoder-std-unmarshal-from-use-number.md ).
var (
	stdFromOptions       = json.DefaultOptionsV1()
	stdFromRejectOptions = stdjson.JoinOptions(stdFromOptions, stdjson.RejectUnknownMembers(true))
)

// stdUnmarshalFrom calls UnmarshalJSONFrom of v, a stdjson.UnmarshalerFrom, with a decoder of the value, as
// unmarshalFrom does.
func stdUnmarshalFrom(v any, value []byte, flags OptionFlags) (bool, error) {
	d := stdFromDecoders.Get().(*stdFromDecoder)
	defer stdFromDecoders.Put(d)
	d.r.Reset(value)
	opts := stdFromOptions
	if flags&DisallowUnknownFieldsOption != 0 {
		opts = stdFromRejectOptions
	}
	d.dec.Reset(&d.r, opts)
	err := v.(stdjson.UnmarshalerFrom).UnmarshalJSONFrom(d.dec)
	_, n := d.dec.StackIndex(0)
	declined, err := errors.MethodCallResult(err, d.dec.StackDepth(), n)
	d.r.Reset(nil)
	return declined, err
}
