//go:build !go1.27 || !goexperiment.jsonv2

package decoder

import "reflect"

// stdUnmarshalerFromType is nil where encoding/json is not built on encoding/json/v2: it calls no UnmarshalJSONFrom,
// and encoding/json/jsontext doesn't exist. stdUnmarshalFrom is nil as well, as no type implements
// stdUnmarshalerFromType.
var (
	stdUnmarshalerFromType reflect.Type
	stdUnmarshalFrom       func(any, []byte, OptionFlags) (bool, error)
)
