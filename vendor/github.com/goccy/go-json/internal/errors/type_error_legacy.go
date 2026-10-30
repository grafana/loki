//go:build !go1.27 || !goexperiment.jsonv2

package errors

import "fmt"

// typeErrorMessage is the message of encoding/json before Go 1.27, and with GOEXPERIMENT=nojsonv2: Struct is the
// struct type which contains the field, and Field the names of the fields from the root to it.
func typeErrorMessage(e *UnmarshalTypeError) string {
	if e.Struct != "" || e.Field != "" {
		return fmt.Sprintf("json: cannot unmarshal %s into Go struct field %s.%s of type %s", e.Value, e.Struct, e.Field, e.Type)
	}
	return fmt.Sprintf("json: cannot unmarshal %s into Go value of type %s", e.Value, e.Type)
}
