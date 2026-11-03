package run

import (
	"reflect"

	"github.com/goccy/go-json/internal/encoder"
	"github.com/goccy/go-json/internal/runtime"
)

// MaxReusedValueSize is the size of the largest value which EncodeOf, and the decoding of a value by its type,
// copy to a value they reuse. Every runtime context in the pool keeps such a value, so a large one is left to the
// GC.
const MaxReusedValueSize = 4096

// EncodeOf encodes the value at v, as Encode encodes it, without the copy of the value to the heap which an
// interface value of it would be: it is copied to a value in the heap which is reused ( see
// encoder.OpcodeSet.TakeValue ). A value which an interface value holds directly is encoded by Encode.
func EncodeOf[T any](ctx *encoder.RuntimeContext, v *T) ([]byte, error) {
	if ctx.Option.Flag&encoder.MarshalFuncsOption != 0 {
		// the code sets of the functions of a call, whose pool of values the runtime keeps for a GC after it is used,
		// would keep the functions alive.
		return Encode(ctx, *v)
	}
	typ := reflect.TypeOf(v).Elem()
	switch typ.Kind() {
	case reflect.Interface:
		// the type to encode is the one of the value which the interface value holds.
		return Encode(ctx, *v)
	case reflect.Ptr, reflect.Map:
		// the value is stored directly in an interface value, which needs no allocation.
		// The other types stored directly are found by their code set below.
		return Encode(ctx, *v)
	}
	codeSet, err := encoder.CompileToGetCodeSet(ctx, uintptr(runtime.TypePtr(typ)))
	if err != nil {
		return nil, err
	}
	if !codeSet.IfaceIndir {
		// the value is stored directly in an interface value, which needs no allocation.
		return Encode(ctx, *v)
	}
	if typ.Size() > MaxReusedValueSize {
		return Encode(ctx, *v)
	}

	p := codeSet.TakeValue(ctx)
	*(*T)(p) = *v
	ctx.Init(p, codeSet.CodeLength)
	buf, err := Code(ctx, ctx.Buf[:0], codeSet)
	// the pool must not keep what the value refers to alive.
	var zero T
	*(*T)(p) = zero
	if err != nil {
		return nil, err
	}
	ctx.Buf = buf
	return buf, nil
}
