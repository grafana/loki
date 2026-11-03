//go:build !go1.27 || !goexperiment.jsonv2

package encoder

import "reflect"

// stdMarshalerToType is nil where encoding/json is not built on encoding/json/v2: it calls no MarshalJSONTo, and
// encoding/json/jsontext doesn't exist. stdMarshalTo is nil as well, as no type implements stdMarshalerToType.
var (
	stdMarshalerToType reflect.Type
	stdMarshalTo       func(any) ([]byte, bool, error)
)
