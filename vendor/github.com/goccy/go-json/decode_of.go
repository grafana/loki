package json

import (
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/decoder"
	"github.com/goccy/go-json/internal/encoder/run"
	"github.com/goccy/go-json/internal/runtime"
)

// UnmarshalOf parses the JSON-encoded data and stores the result in the value pointed to by v, as Unmarshal does.
//
// Unmarshal takes its argument as an interface value, and the decoder refers to the value by its address,
// so the value always escapes to the heap. UnmarshalOf takes the pointer by its type and never gives it to
// the decoder: it copies *v to a value in the heap which is reused, decodes into that value, and copies the
// result back to *v. So v may point to a variable on the stack of the caller, and a value which needs no
// allocation of its own ( no pointer, slice, map or string to fill ) is decoded without an allocation. With
// DecodeNoCopyString, a string without an escape refers to data, and needs none either.
//
// A value is copied twice, so a large value is faster with Unmarshal when it is in the heap anyway. As the
// result is a copy, an UnmarshalJSON or UnmarshalText method of a type in the value must not keep the address
// of its receiver, which is the address of the copy.
func UnmarshalOf[T any](data []byte, v *T, optFuncs ...DecodeOptionFunc) error {
	ptrType := reflect.TypeOf((*T)(nil))
	if v == nil {
		return &InvalidUnmarshalError{Type: ptrType}
	}
	ctx := decoder.TakeRuntimeContext()
	dec, err := ctx.DecoderOf(runtime.TypePtr(ptrType))
	if err != nil {
		decoder.ReleaseRuntimeContext(ctx)
		return err
	}
	var p unsafe.Pointer
	if unsafe.Sizeof(*v) > run.MaxReusedValueSize {
		// every context of the pool would keep such a value: it is left to the GC.
		p = reflect.New(ptrType.Elem()).UnsafePointer()
	} else {
		p = ctx.TakeValue(ptrType.Elem(), runtime.TypePtr(ptrType.Elem()))
	}
	*(*T)(p) = *v

	src := ctx.SetInput(data)
	ctx.Option.Flags = 0
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}
	cursor, err := dec.Decode(ctx, 0, 0, p)
	if err == nil {
		// a type error is returned only when the whole input is valid, as encoding/json does
		if err = validateEndBuf(src, cursor); err == nil && ctx.HasTypeError() {
			err = ctx.TypeError(reflect.TypeOf((*T)(nil)), 0, 0, nil)
		}
	}
	if err != nil {
		err = inputSyntaxError(ctx, err)
	}
	ctx.DiscardTypeError()
	// What was decoded before an error is stored too, as Unmarshal does.
	*v = *(*T)(p)
	// the pool must not keep what the value refers to alive.
	var zero T
	*(*T)(p) = zero
	decoder.ReleaseRuntimeContext(ctx)
	return err
}
