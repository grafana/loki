//go:build !go1.27 || !goexperiment.jsonv2

package encoder

// invalidUTF8 is what a byte of a string which is not valid UTF-8 is replaced by: encoding/json before Go 1.27,
// and with GOEXPERIMENT=nojsonv2, writes the replacement character U+FFFD escaped.
const invalidUTF8 = `\ufffd`
