//go:build !appengine && !appenginevm
// +build !appengine,!appenginevm

package jsonparser

import (
	"reflect"
	"runtime"
	"strconv"
	"unsafe"
)

func parseFloat(b *[]byte) (float64, error) {
	return strconv.ParseFloat(*(*string)(unsafe.Pointer(b)), 64)
}

// A hack until issue golang/go#2632 is fixed.
// See: https://github.com/golang/go/issues/2632
func bytesToString(b *[]byte) string {
	return *(*string)(unsafe.Pointer(b))
}

func StringToBytes(s string) []byte {
	b := make([]byte, 0, 0)
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh.Data = sh.Data
	bh.Cap = sh.Len
	bh.Len = sh.Len
	runtime.KeepAlive(s)
	return b
}
