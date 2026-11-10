//go:build go1.27 && goexperiment.jsonv2

package encoder

import (
	"bytes"
	jsonv1 "encoding/json"
	stdjsontext "encoding/json/jsontext"
	stdjson "encoding/json/v2"
	"reflect"
	"sync"

	"github.com/goccy/go-json/internal/errors"
)

// stdMarshalerToType is the interface of MarshalJSONTo with the Encoder of encoding/json/jsontext, which encoding/json
// calls where it is built on encoding/json/v2.
var stdMarshalerToType = reflect.TypeFor[stdjson.MarshalerTo]()

// stdToEncoder is an encoder of encoding/json/jsontext which a MarshalJSONTo method writes to, and the buffer of its
// output.
type stdToEncoder struct {
	buf bytes.Buffer
	enc *stdjsontext.Encoder
}

var stdToEncoders = sync.Pool{New: func() any {
	e := new(stdToEncoder)
	e.enc = stdjsontext.NewEncoder(&e.buf)
	return e
}}

// stdToOptions are the options of the encoder which MarshalJSONTo of encoding/json/jsontext writes to, as toConfig:
// DefaultOptionsV1 of encoding/json, without the escapes, which are made after the method.
var stdToOptions = stdjson.JoinOptions(jsonv1.DefaultOptionsV1(), stdjsontext.EscapeForHTML(false), stdjsontext.EscapeForJS(false))

// stdMarshalTo returns what MarshalJSONTo of v, a stdjson.MarshalerTo, writes, and whether it declined, as
// marshalTo does.
func stdMarshalTo(v any) ([]byte, bool, error) {
	e := stdToEncoders.Get().(*stdToEncoder)
	defer stdToEncoders.Put(e)
	e.buf.Reset()
	e.enc.Reset(&e.buf, stdToOptions)
	err := v.(stdjson.MarshalerTo).MarshalJSONTo(e.enc)
	_, n := e.enc.StackIndex(0)
	declined, err := errors.MethodCallResult(err, e.enc.StackDepth(), n)
	if err != nil || declined {
		return nil, declined, err
	}
	return bytes.Clone(bytes.TrimSuffix(e.buf.Bytes(), []byte{'\n'})), false, nil
}
