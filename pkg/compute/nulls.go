package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// PropagateNulls returns a shallow copy of input whose validity is intersected
// with validity. Input must be an Array. The returned Array shares its value
// buffers with input.
func PropagateNulls(alloc *memory.Allocator, input columnar.Datum, validity memory.Bitmap) (columnar.Datum, error) {
	arr, ok := input.(columnar.Array)
	if !ok {
		return nil, fmt.Errorf("PropagateNulls requires an Array input, got %T", input)
	}
	if validity.Len() != 0 && validity.Len() != arr.Len() {
		return nil, fmt.Errorf("validity bitmap length mismatch: %d != %d", validity.Len(), arr.Len())
	}

	resultValidity, err := computeValidityAA(alloc, arr.Validity(), validity)
	if err != nil {
		return nil, err
	}

	switch input := arr.(type) {
	case *columnar.Null:
		return columnar.NewNull(resultValidity), nil
	case *columnar.Bool:
		return columnar.NewBool(input.Values(), resultValidity), nil
	case *columnar.Number[int32]:
		return columnar.NewNumber(input.Values(), resultValidity), nil
	case *columnar.Number[int64]:
		return columnar.NewNumber(input.Values(), resultValidity), nil
	case *columnar.Number[uint32]:
		return columnar.NewNumber(input.Values(), resultValidity), nil
	case *columnar.Number[uint64]:
		return columnar.NewNumber(input.Values(), resultValidity), nil
	case *columnar.UTF8:
		return columnar.NewUTF8(input.Data(), input.Offsets(), resultValidity), nil
	case *columnar.Struct:
		fields := make([]columnar.Array, input.NumFields())
		for i := range fields {
			fields[i] = input.Field(i)
		}
		return columnar.NewStruct(input.Schema(), fields, input.Len(), resultValidity), nil
	default:
		panic(fmt.Sprintf("unexpected Array type %T", input))
	}
}
