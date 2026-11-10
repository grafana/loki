//go:build !go1.24

package encoder

import (
	"reflect"
	"unsafe"
)

// stdJSONAppender returns nil: time.Time has AppendText from Go 1.24 on.
func stdJSONAppender(_ reflect.Type) func([]byte, unsafe.Pointer) ([]byte, bool) {
	return nil
}
