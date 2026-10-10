//go:build go1.27 && goexperiment.jsonv2

package encoder

// invalidUTF8 is what a byte of a string which is not valid UTF-8 is replaced by: encoding/json of Go 1.27,
// which is made of encoding/json/v2, writes the replacement character U+FFFD as it is, without an escape.
const invalidUTF8 = "\ufffd"
