//go:build go1.27 && goexperiment.jsonv2

package errors

import "strings"

// typeErrorMessage is the message of encoding/json of Go 1.27, which is made of encoding/json/v2: Struct is the
// root type, and Field the path from it to the value, whose array elements are their indices. A path which ends
// with an index is not called a struct field, and the cause of the error, Err, follows the message.
func typeErrorMessage(e *UnmarshalTypeError) string {
	var s string
	if e.Struct != "" || e.Field != "" {
		intoWhat := "Go struct field "
		last := e.Field[strings.LastIndexByte(e.Field, '.')+1:]
		if last != "" && strings.TrimRight(last, "0123456789") == "" {
			intoWhat = ""
		}
		s = "json: cannot unmarshal " + e.Value + " into " + intoWhat + e.Struct + "." + e.Field + " of type " + e.Type.String()
	} else {
		s = "json: cannot unmarshal " + e.Value + " into Go value of type " + e.Type.String()
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}
