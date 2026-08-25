package columnar

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/memory"
)

// Clone returns a deep copy of the input Datum, allocating all new memory from
// alloc. The returned Datum does not share any memory with the input.
//
// Scalars are returned as-is since they do not reference allocator-managed
// memory.
func Clone(alloc *memory.Allocator, input Datum) (Datum, error) {
	switch src := input.(type) {
	case Scalar:
		return src, nil

	case *Null:
		return cloneNull(alloc, src), nil
	case *Bool:
		return cloneBool(alloc, src), nil
	case *Number[int32]:
		return cloneNumber(alloc, src), nil
	case *Number[int64]:
		return cloneNumber(alloc, src), nil
	case *Number[uint32]:
		return cloneNumber(alloc, src), nil
	case *Number[uint64]:
		return cloneNumber(alloc, src), nil
	case *UTF8:
		return cloneUTF8(alloc, src), nil
	case *Struct:
		return cloneStruct(alloc, src)

	default:
		return nil, fmt.Errorf("Clone: unsupported datum type %T", input)
	}
}

func cloneNull(alloc *memory.Allocator, src *Null) *Null {
	validity := cloneValidity(alloc, src.Validity())
	return NewNull(validity)
}

func cloneValidity(alloc *memory.Allocator, validity memory.Bitmap) memory.Bitmap {
	if validity.Len() == 0 {
		return memory.Bitmap{}
	}
	return *validity.Clone(alloc)
}

func cloneBool(alloc *memory.Allocator, src *Bool) *Bool {
	validity := cloneValidity(alloc, src.Validity())
	srcValues := src.Values()
	values := *srcValues.Clone(alloc)
	return NewBool(values, validity)
}

func cloneNumber[T Numeric](alloc *memory.Allocator, src *Number[T]) *Number[T] {
	validity := cloneValidity(alloc, src.Validity())
	values := memory.NewBuffer[T](alloc, src.Len())
	values.Append(src.Values()...)
	return NewNumber(values.Data(), validity)
}

func cloneStruct(alloc *memory.Allocator, src *Struct) (*Struct, error) {
	fields := make([]Array, src.NumFields())
	for i := range fields {
		cloned, err := Clone(alloc, src.Field(i))
		if err != nil {
			return nil, fmt.Errorf("field %d: %w", i, err)
		}
		fields[i] = cloned.(Array)
	}
	validity := cloneValidity(alloc, src.Validity())
	return NewStruct(src.Schema(), fields, src.Len(), validity), nil
}

func cloneUTF8(alloc *memory.Allocator, src *UTF8) *UTF8 {
	// Normalize first so offsets are zero-based and data is trimmed.
	normalized := src.Normalize(alloc)
	if normalized != src {
		// Normalize already produced a full copy; return directly.
		return normalized
	}

	var (
		srcData    = src.Data()[:src.DataLen()]
		srcOffsets = src.Offsets()
	)

	data := memory.NewBuffer[byte](alloc, len(srcData))
	data.Append(srcData...)

	offsets := memory.NewBuffer[int32](alloc, len(srcOffsets))
	offsets.Append(srcOffsets...)

	validity := cloneValidity(alloc, src.Validity())
	return NewUTF8(data.Data(), offsets.Data(), validity)
}
