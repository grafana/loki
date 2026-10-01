package json

import (
	"reflect"

	"github.com/goccy/go-json/internal/encoder"
	"github.com/goccy/go-json/internal/runtime"
)

// MarshalOf returns the JSON encoding of v, as Marshal does.
//
// Marshal takes its argument as an interface value, so a value which is not a pointer is copied to the heap
// for every call. MarshalOf takes the value by its type, and it copies the value to a value in the heap which is
// reused, so it encodes a value without an allocation other than the one of the result.
//
// Which one to use depends on what is passed:
//   - A value which is not a pointer ( a struct, an int, a string, ... ): MarshalOf(v) is faster than Marshal(v),
//     because it saves the allocation. It also lets v stay on the stack of the caller, which Marshal(&v) doesn't.
//   - A pointer or a map: they are the same, because such a value is stored in an interface value without
//     an allocation.
//   - A large value which is already referred to by a pointer p: Marshal(p) is the fastest. MarshalOf(*p) copies
//     the whole value, which costs more as the value gets larger.
func MarshalOf[T any](v T, optFuncs ...EncodeOptionFunc) ([]byte, error) {
	ctx := encoder.TakeRuntimeContext()

	ctx.Option.Flag = 0
	ctx.Option.Flag |= (encoder.HTMLEscapeOption | encoder.NormalizeUTF8Option)
	for _, optFunc := range optFuncs {
		optFunc(ctx.Option)
	}

	buf, err := encodeOf(ctx, &v)
	if err != nil {
		encoder.ReleaseRuntimeContext(ctx)
		return nil, err
	}

	// see marshal for the reason why the result is copied this way.
	buf = buf[:len(buf)-1]
	copied := make([]byte, len(buf))
	copy(copied, buf)

	encoder.ReleaseRuntimeContext(ctx)
	return copied, nil
}

// maxReusedValueSize is the size of the largest value which MarshalOf copies to a value it reuses.
// Every runtime context in the pool keeps such a value, so a large one is left to the GC.
const maxReusedValueSize = 4096

func encodeOf[T any](ctx *encoder.RuntimeContext, v *T) ([]byte, error) {
	typ := reflect.TypeOf(v).Elem()
	switch typ.Kind() {
	case reflect.Interface:
		// the type to encode is the one of the value which the interface value holds.
		return encode(ctx, *v)
	case reflect.Ptr, reflect.Map:
		// the value is stored directly in an interface value, which needs no allocation.
		// The other types stored directly are found by their code set below.
		return encode(ctx, *v)
	}
	codeSet, err := encoder.CompileToGetCodeSet(ctx, uintptr(runtime.TypePtr(typ)))
	if err != nil {
		return nil, err
	}
	if !codeSet.IfaceIndir {
		// the value is stored directly in an interface value, which needs no allocation.
		return encode(ctx, *v)
	}
	if typ.Size() > maxReusedValueSize {
		return encode(ctx, *v)
	}

	p := codeSet.TakeValue(ctx)
	*(*T)(p) = *v
	ctx.Init(p, codeSet.CodeLength)
	buf, err := encodeRunCode(ctx, ctx.Buf[:0], codeSet)
	// the pool must not keep what the value refers to alive.
	var zero T
	*(*T)(p) = zero
	if err != nil {
		return nil, err
	}
	ctx.Buf = buf
	return buf, nil
}
