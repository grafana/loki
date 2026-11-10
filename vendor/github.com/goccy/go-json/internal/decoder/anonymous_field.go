package decoder

import (
	"reflect"
	"unsafe"
)

type anonymousFieldDecoder struct {
	structType reflect.Type
	offset     uintptr
	dec        Decoder
}

func newAnonymousFieldDecoder(structType reflect.Type, offset uintptr, dec Decoder) *anonymousFieldDecoder {
	return &anonymousFieldDecoder{
		structType: structType,
		offset:     offset,
		dec:        dec,
	}
}

func (d *anonymousFieldDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	if *(*unsafe.Pointer)(p) == nil {
		*(*unsafe.Pointer)(p) = newValue(d.structType)
	}
	p = *(*unsafe.Pointer)(p)
	return d.dec.Decode(ctx, cursor, depth, unsafe.Add(p, d.offset))
}

func (d *anonymousFieldDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return d.dec.DecodePath(ctx, cursor, depth)
}
