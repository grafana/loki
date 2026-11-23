package decoder

import (
	"fmt"
	"reflect"
	"unsafe"
)

type ptrDecoder struct {
	dec        Decoder
	typ        reflect.Type
	structName string
	fieldName  string
}

// newPtrDecoder returns the decoder of a pointer to typ: a basicPtrDecoder if typ has an allocator of its own
// ( see allocatorOf ), and else a ptrDecoder.
func newPtrDecoder(dec Decoder, typ reflect.Type, structName, fieldName string) Decoder {
	d := ptrDecoder{
		dec:        dec,
		typ:        typ,
		structName: structName,
		fieldName:  fieldName,
	}
	if alloc := allocatorOf(typ); alloc != nil {
		return &basicPtrDecoder{ptrDecoder: d, alloc: alloc}
	}
	return &d
}

func (d *ptrDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if p != nil {
			*(*unsafe.Pointer)(p) = nil
		}
		cursor += 4
		return cursor, nil
	}
	var newptr unsafe.Pointer
	if *(*unsafe.Pointer)(p) == nil {
		newptr = newValue(d.typ)
		*(*unsafe.Pointer)(p) = newptr
	} else {
		newptr = *(*unsafe.Pointer)(p)
	}
	// after an error, the pointer is left as it is, as encoding/json leaves it; the cursor is the one of the decoder:
	// after a key of a map of another type, the map goes on from it ( see errMapKeyType )
	return d.dec.Decode(ctx, cursor, depth, newptr)
}

// basicPtrDecoder is the decoder of a pointer to a type which has an allocator of its own ( see allocatorOf ),
// which allocates the value a nil pointer is set to. It is a type of its own, so that the decoder of a pointer
// to any other type is the one it was.
type basicPtrDecoder struct {
	ptrDecoder
	alloc func() unsafe.Pointer
}

func (d *basicPtrDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if p != nil {
			*(*unsafe.Pointer)(p) = nil
		}
		return cursor + 4, nil
	}
	newptr := *(*unsafe.Pointer)(p)
	if newptr == nil {
		newptr = d.alloc()
		*(*unsafe.Pointer)(p) = newptr
	}
	// as ptrDecoder.Decode
	return d.dec.Decode(ctx, cursor, depth, newptr)
}

func (d *ptrDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: ptr decoder does not support decode path")
}
