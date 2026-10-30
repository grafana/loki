// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build go1.18

package kernels

import (
	"fmt"
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

func listElementOutputType(_ *exec.KernelCtx, inputTypes []arrow.DataType) (arrow.DataType, error) {
	listType, ok := inputTypes[0].(arrow.ListLikeType)
	if !ok {
		return nil, fmt.Errorf("%w: list_element requires a list-like input", arrow.ErrType)
	}
	return listType.Elem(), nil
}

func getListElementIndex(value *exec.ExecValue) (uint64, error) {
	if value.IsScalar() {
		return listElementScalarIndex(value.Scalar)
	}

	if value.Array.Len == 0 {
		return 0, fmt.Errorf("%w: list_element index array is empty", arrow.ErrInvalid)
	}
	if value.Array.Len > 1 {
		return 0, fmt.Errorf("%w: list_element does not support arrays of list indices", arrow.ErrNotImplemented)
	}
	if value.Array.UpdateNullCount() != 0 {
		return 0, fmt.Errorf("%w: list_element index must not contain nulls", arrow.ErrInvalid)
	}

	switch value.Array.Type.ID() {
	case arrow.INT8:
		return unsignedIndex(exec.GetSpanValues[int8](&value.Array, 1)[0])
	case arrow.INT16:
		return unsignedIndex(exec.GetSpanValues[int16](&value.Array, 1)[0])
	case arrow.INT32:
		return unsignedIndex(exec.GetSpanValues[int32](&value.Array, 1)[0])
	case arrow.INT64:
		return unsignedIndex(exec.GetSpanValues[int64](&value.Array, 1)[0])
	case arrow.UINT8:
		return uint64(exec.GetSpanValues[uint8](&value.Array, 1)[0]), nil
	case arrow.UINT16:
		return uint64(exec.GetSpanValues[uint16](&value.Array, 1)[0]), nil
	case arrow.UINT32:
		return uint64(exec.GetSpanValues[uint32](&value.Array, 1)[0]), nil
	case arrow.UINT64:
		return exec.GetSpanValues[uint64](&value.Array, 1)[0], nil
	default:
		return 0, fmt.Errorf("%w: invalid list_element index type %s", arrow.ErrType, value.Array.Type)
	}
}

func ValidateListElementScalarIndex(value scalar.Scalar) error {
	_, err := listElementScalarIndex(value)
	return err
}

func ListElementScalarIndex(value scalar.Scalar) (uint64, error) {
	return listElementScalarIndex(value)
}

func listElementScalarIndex(value scalar.Scalar) (uint64, error) {
	if !value.IsValid() {
		return 0, fmt.Errorf("%w: list_element index must not be null", arrow.ErrInvalid)
	}
	return scalarIndex(value)
}

func scalarIndex(value scalar.Scalar) (uint64, error) {
	switch value := value.(type) {
	case *scalar.Int8:
		return unsignedIndex(value.Value)
	case *scalar.Int16:
		return unsignedIndex(value.Value)
	case *scalar.Int32:
		return unsignedIndex(value.Value)
	case *scalar.Int64:
		return unsignedIndex(value.Value)
	case *scalar.Uint8:
		return uint64(value.Value), nil
	case *scalar.Uint16:
		return uint64(value.Value), nil
	case *scalar.Uint32:
		return uint64(value.Value), nil
	case *scalar.Uint64:
		return value.Value, nil
	default:
		return 0, fmt.Errorf("%w: invalid list_element index type %s", arrow.ErrType, value.DataType())
	}
}

func unsignedIndex[T arrow.IntType](value T) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%w: list_element index %d is out of bounds: should be greater than or equal to 0", arrow.ErrInvalid, value)
	}
	return uint64(value), nil
}

func listElementValueOffsets(list *exec.ArraySpan, i int64) (int64, int64, error) {
	check := func(start, end int64) (int64, int64, error) {
		if start < 0 || end < start || end > list.Children[0].Len {
			return 0, 0, fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		return start, end, nil
	}

	switch list.Type.ID() {
	case arrow.LIST:
		offsets := exec.GetSpanOffsets[int32](list, 1)
		return check(int64(offsets[i]), int64(offsets[i+1]))
	case arrow.LARGE_LIST:
		offsets := exec.GetSpanOffsets[int64](list, 1)
		return check(offsets[i], offsets[i+1])
	case arrow.LIST_VIEW:
		offsets := exec.GetSpanValues[int32](list, 1)
		sizes := exec.GetSpanValues[int32](list, 2)
		start := int64(offsets[i])
		size := int64(sizes[i])
		if size < 0 {
			return 0, 0, fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		return check(start, start+size)
	case arrow.LARGE_LIST_VIEW:
		offsets := exec.GetSpanValues[int64](list, 1)
		sizes := exec.GetSpanValues[int64](list, 2)
		start := offsets[i]
		size := sizes[i]
		if start < 0 || size < 0 || size > math.MaxInt64-start {
			return 0, 0, fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		return check(start, start+size)
	case arrow.FIXED_SIZE_LIST:
		size := int64(list.Type.(*arrow.FixedSizeListType).Len())
		if list.Offset < 0 || i < 0 || i > math.MaxInt64-list.Offset {
			return 0, 0, fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		position := list.Offset + i
		if size < 0 || (size > 0 && position > math.MaxInt64/size) {
			return 0, 0, fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		start := position * size
		if size > math.MaxInt64-start {
			return 0, 0, fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		return check(start, start+size)
	default:
		return 0, 0, fmt.Errorf("%w: unsupported list_element input type %s", arrow.ErrType, list.Type)
	}
}

func listElementExec(ctx *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	var listSpan exec.ArraySpan
	if batch.Values[0].IsScalar() {
		listSpan.FillFromScalar(batch.Values[0].Scalar)
	} else {
		listSpan = batch.Values[0].Array
	}
	list := &listSpan
	if len(list.Children) == 0 {
		return fmt.Errorf("%w: list_element input has no values child", arrow.ErrInvalid)
	}

	index, err := getListElementIndex(&batch.Values[1])
	if err != nil {
		return err
	}

	elemType := list.Type.(arrow.ListLikeType).Elem()
	if !ListElementOutputTypeSupported(elemType) {
		return fmt.Errorf("%w: list_element output type %s is not supported", arrow.ErrNotImplemented, elemType)
	}
	if list.Len == 0 {
		values := list.Children[0].MakeArray()
		defer values.Release()
		empty := array.NewSlice(values, 0, 0)
		defer empty.Release()
		out.TakeOwnership(empty.Data())
		return nil
	}
	if !listElementTakeSupported(elemType) {
		return listElementConcat(ctx, list, index, elemType, out)
	}

	indexBuilder := array.NewInt64Builder(exec.GetAllocator(ctx.Ctx))
	defer indexBuilder.Release()
	indexBuilder.Reserve(int(list.Len))
	for i := int64(0); i < list.Len; i++ {
		if len(list.Buffers[0].Buf) != 0 && bitutil.BitIsNotSet(list.Buffers[0].Buf, int(list.Offset+i)) {
			indexBuilder.AppendNull()
			continue
		}

		start, end, err := listElementValueOffsets(list, i)
		if err != nil {
			return err
		}
		if end < start {
			return fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		length := uint64(end - start)
		if index >= length {
			return fmt.Errorf("%w: list_element index %d is out of bounds: should be in [0, %d)", arrow.ErrInvalid, index, length)
		}
		indexBuilder.Append(start + int64(index))
	}

	indices := indexBuilder.NewArray()
	defer indices.Release()
	return listElementTakeOrFallback(ctx, &list.Children[0], indices, out)
}

func ListElementOutputTypeSupported(typ arrow.DataType) bool {
	switch typ.ID() {
	case arrow.BINARY_VIEW, arrow.STRING_VIEW:
		return false
	case arrow.SPARSE_UNION, arrow.DENSE_UNION:
		if typ.(arrow.UnionType).NumFields() == 0 {
			return false
		}
	case arrow.EXTENSION:
		storageType := typ.(arrow.ExtensionType).StorageType()
		// ArraySpan does not preserve a dictionary stored under an extension.
		return storageType.ID() != arrow.DICTIONARY && ListElementOutputTypeSupported(storageType)
	case arrow.DICTIONARY:
		return ListElementOutputTypeSupported(typ.(*arrow.DictionaryType).ValueType)
	}

	nested, ok := typ.(arrow.NestedType)
	if !ok {
		return true
	}
	for _, field := range nested.Fields() {
		if !ListElementOutputTypeSupported(field.Type) {
			return false
		}
	}
	return true
}

func listElementTakeSupported(typ arrow.DataType) bool {
	id := typ.ID()
	if id == arrow.NULL || arrow.IsBinaryLike(id) || arrow.IsLargeBinaryLike(id) ||
		arrow.IsFixedSizeBinary(id) || id == arrow.SPARSE_UNION || id == arrow.DENSE_UNION ||
		id == arrow.EXTENSION {
		return true
	}
	if !arrow.IsPrimitive(id) {
		return false
	}

	// PrimitiveTake has specialized implementations for these widths only.
	// In particular, INTERVAL_MONTH_DAY_NANO is a primitive 128-bit type and
	// must use the generic concatenation fallback below.
	fixed, ok := typ.(arrow.FixedWidthDataType)
	if !ok {
		return false
	}
	switch fixed.BitWidth() {
	case 1, 8, 16, 32, 64:
		return true
	default:
		return false
	}
}

func listElementConcat(ctx *exec.KernelCtx, list *exec.ArraySpan, index uint64, elemType arrow.DataType, out *exec.ExecResult) error {
	values := list.Children[0].MakeArray()
	defer values.Release()
	pieces := make([]arrow.Array, 0, int(list.Len))
	defer func() {
		for _, piece := range pieces {
			piece.Release()
		}
	}()

	for i := int64(0); i < list.Len; i++ {
		if len(list.Buffers[0].Buf) != 0 && bitutil.BitIsNotSet(list.Buffers[0].Buf, int(list.Offset+i)) {
			pieces = append(pieces, listElementMakeNullLike(ctx, values, elemType))
			continue
		}

		start, end, err := listElementValueOffsets(list, i)
		if err != nil {
			return err
		}
		if end < start {
			return fmt.Errorf("%w: list_element input has invalid value offsets", arrow.ErrInvalid)
		}
		length := uint64(end - start)
		if index >= length {
			return fmt.Errorf("%w: list_element index %d is out of bounds: should be in [0, %d)", arrow.ErrInvalid, index, length)
		}
		selected := start + int64(index)
		pieces = append(pieces, array.NewSlice(values, selected, selected+1))
	}

	result, err := array.Concatenate(pieces, exec.GetAllocator(ctx.Ctx))
	if err != nil {
		return err
	}
	defer result.Release()
	out.TakeOwnership(result.Data())
	return nil
}

func listElementMakeNullLike(ctx *exec.KernelCtx, values arrow.Array, elemType arrow.DataType) arrow.Array {
	mem := exec.GetAllocator(ctx.Ctx)
	if extType, ok := elemType.(arrow.ExtensionType); ok {
		storageValues := values
		if extValues, ok := values.(array.ExtensionArray); ok {
			storageValues = extValues.Storage()
		}
		storage := listElementMakeNullLike(ctx, storageValues, extType.StorageType())
		result := array.NewExtensionArrayWithStorage(extType, storage)
		storage.Release()
		return result
	}
	if elemType.ID() == arrow.RUN_END_ENCODED {
		runEndType := elemType.(*arrow.RunEndEncodedType)
		// Build only the run ends; nested encoded values may not have a builder.
		builder := array.NewRunEndEncodedBuilder(mem, runEndType.RunEnds(), arrow.Null)
		defer builder.Release()
		builder.AppendNull()
		runEnds := builder.NewRunEndEncodedArray()
		defer runEnds.Release()
		nulls := listElementMakeNullLike(ctx, values.(*array.RunEndEncoded).Values(), runEndType.Encoded())
		defer nulls.Release()
		return array.NewRunEndEncodedArrayWithType(runEndType, runEnds.RunEndsArr(), nulls, 1, 0)
	}
	if values.Len() == 0 || len(values.Data().Buffers()) == 0 ||
		elemType.ID() == arrow.NULL || arrow.IsUnion(elemType.ID()) {
		return array.MakeArrayOfNull(mem, elemType, 1)
	}

	source := array.NewSlice(values, 0, 1)
	defer source.Release()

	sourceData := source.Data()
	validity := memory.NewResizableBuffer(mem)
	validity.Resize(int(bitutil.BytesForBits(int64(sourceData.Offset() + 1))))
	memory.Set(validity.Bytes(), 0)
	defer validity.Release()

	buffers := append([]*memory.Buffer(nil), sourceData.Buffers()...)
	buffers[0] = validity
	data := array.NewData(sourceData.DataType(), 1, buffers, sourceData.Children(), 1, sourceData.Offset())
	if dictionary := sourceData.Dictionary(); dictionary != nil {
		data.SetDictionary(dictionary)
	}
	defer data.Release()
	return array.MakeFromData(data)
}

func listElementTakeFallback(ctx *exec.KernelCtx, values *exec.ArraySpan, indices arrow.Array, out *exec.ExecResult) error {
	elemType := values.Type
	valuesArray := values.MakeArray()
	defer valuesArray.Release()
	if indices.Len() == 0 {
		empty := array.NewSlice(valuesArray, 0, 0)
		defer empty.Release()
		out.TakeOwnership(empty.Data())
		return nil
	}

	pieces := make([]arrow.Array, 0, indices.Len())
	defer func() {
		for _, piece := range pieces {
			piece.Release()
		}
	}()

	for i := 0; i < indices.Len(); i++ {
		if indices.IsNull(i) {
			pieces = append(pieces, listElementMakeNullLike(ctx, valuesArray, elemType))
			continue
		}
		selected, err := listElementTakeIndex(indices, i)
		if err != nil {
			return err
		}
		pieces = append(pieces, array.NewSlice(valuesArray, selected, selected+1))
	}

	result, err := array.Concatenate(pieces, exec.GetAllocator(ctx.Ctx))
	if err != nil {
		return err
	}
	defer result.Release()
	out.TakeOwnership(result.Data())
	return nil
}

func listElementTakeIndex(indices arrow.Array, i int) (int64, error) {
	switch indexArray := indices.(type) {
	case *array.Int32:
		return int64(indexArray.Value(i)), nil
	case *array.Int64:
		return indexArray.Value(i), nil
	default:
		return 0, fmt.Errorf("%w: list_element fallback received unsupported index type %s", arrow.ErrType, indices.DataType())
	}
}

func listElementTakeOrFallback(ctx *exec.KernelCtx, values *exec.ArraySpan, indices arrow.Array, out *exec.ExecResult) error {
	if indices.Len() == 0 {
		return listElementTakeFallback(ctx, values, indices, out)
	}
	if handled, err := listElementTake(ctx, values, indices, out); handled {
		return err
	}
	return listElementTakeFallback(ctx, values, indices, out)
}

func listElementDenseUnionTake(ctx *exec.KernelCtx, values *exec.ArraySpan, indices arrow.Array, out *exec.ExecResult) error {
	var indexSpan exec.ArraySpan
	indexSpan.SetMembers(indices.Data())
	batch := &exec.ExecSpan{
		Len: int64(indices.Len()),
		Values: []exec.ExecValue{
			{Array: *values},
			{Array: indexSpan},
		},
	}
	takeCtx := *ctx
	takeCtx.State = TakeOptions{BoundsCheck: false}
	if err := TakeExec(DenseUnionImpl)(&takeCtx, batch, out); err != nil {
		return err
	}

	for i := range out.Children {
		childIndices := out.Children[i].MakeArray()
		out.Children[i] = exec.ArraySpan{}
		childOut := &exec.ExecResult{Type: values.Children[i].Type}
		err := listElementTakeOrFallback(ctx, &values.Children[i], childIndices, childOut)
		childIndices.Release()
		if err != nil {
			childOut.Release()
			return err
		}

		childData := childOut.MakeData()
		out.Children[i].TakeOwnership(childData)
		childData.Release()
	}
	return nil
}

func listElementSparseUnionTake(ctx *exec.KernelCtx, values *exec.ArraySpan, indices arrow.Array, out *exec.ExecResult) error {
	valuesArray := values.MakeArray()
	defer valuesArray.Release()

	union := valuesArray.(*array.SparseUnion)
	unionType := values.Type.(*arrow.SparseUnionType)
	typeIDBuilder := array.NewInt8Builder(exec.GetAllocator(ctx.Ctx))
	defer typeIDBuilder.Release()
	typeIDBuilder.Reserve(indices.Len())

	for i := 0; i < indices.Len(); i++ {
		if indices.IsNull(i) {
			typeIDBuilder.Append(unionType.TypeCodes()[0])
		} else {
			selected, err := listElementTakeIndex(indices, i)
			if err != nil {
				return err
			}
			typeIDBuilder.Append(union.TypeCode(int(selected)))
		}
	}

	typeIDs := typeIDBuilder.NewArray()
	defer typeIDs.Release()

	children := make([]arrow.Array, len(values.Children))
	defer func() {
		for _, child := range children {
			if child != nil {
				child.Release()
			}
		}
	}()
	for i := range values.Children {
		field := union.Field(i)
		var childSpan exec.ArraySpan
		childSpan.SetMembers(field.Data())
		childOut := &exec.ExecResult{Type: childSpan.Type}
		if err := listElementTakeOrFallback(ctx, &childSpan, indices, childOut); err != nil {
			childOut.Release()
			return err
		}
		children[i] = childOut.MakeArray()
	}

	childData := make([]arrow.ArrayData, len(children))
	for i, child := range children {
		childData[i] = child.Data()
	}
	data := array.NewData(unionType, indices.Len(),
		[]*memory.Buffer{nil, typeIDs.Data().Buffers()[1]}, childData, 0, 0)
	result := array.NewSparseUnionData(data)
	data.Release()
	defer result.Release()
	out.TakeOwnership(result.Data())
	return nil
}

func listElementTake(ctx *exec.KernelCtx, values *exec.ArraySpan, indices arrow.Array, out *exec.ExecResult) (bool, error) {
	if !listElementTakeSupported(values.Type) {
		return false, nil
	}

	var indexSpan exec.ArraySpan
	indexSpan.SetMembers(indices.Data())
	batch := &exec.ExecSpan{
		Len: int64(indices.Len()),
		Values: []exec.ExecValue{
			{Array: *values},
			{Array: indexSpan},
		},
	}
	takeCtx := *ctx
	takeCtx.State = TakeOptions{BoundsCheck: false}

	switch id := values.Type.ID(); {
	case id == arrow.NULL:
		return true, NullTake(&takeCtx, batch, out)
	case arrow.IsPrimitive(id):
		return true, PrimitiveTake(&takeCtx, batch, out)
	case arrow.IsBinaryLike(id):
		return true, TakeExec(VarBinaryImpl[int32])(&takeCtx, batch, out)
	case arrow.IsLargeBinaryLike(id):
		return true, TakeExec(VarBinaryImpl[int64])(&takeCtx, batch, out)
	case arrow.IsFixedSizeBinary(id):
		return true, TakeExec(FSBImpl)(&takeCtx, batch, out)
	case id == arrow.SPARSE_UNION:
		return true, listElementSparseUnionTake(ctx, values, indices, out)
	case id == arrow.DENSE_UNION:
		return true, listElementDenseUnionTake(ctx, values, indices, out)
	case id == arrow.EXTENSION:
		extType := values.Type.(arrow.ExtensionType)
		storage := *values
		storage.Type = extType.StorageType()
		handled, err := listElementTake(ctx, &storage, indices, out)
		if handled {
			// The storage take produces the physical buffers and children. Restore
			// the logical extension type so ArraySpan.MakeData reconstructs an
			// ExtensionArray around them.
			out.Type = values.Type
		}
		return handled, err
	default:
		return false, nil
	}
}

func GetListElementKernels() []exec.ScalarKernel {
	kernels := make([]exec.ScalarKernel, 0, 5)
	for _, listID := range []arrow.Type{
		arrow.LIST,
		arrow.LARGE_LIST,
		arrow.LIST_VIEW,
		arrow.LARGE_LIST_VIEW,
		arrow.FIXED_SIZE_LIST,
	} {
		kernel := exec.NewScalarKernel(
			[]exec.InputType{
				exec.NewIDInput(listID),
				exec.NewMatchedInput(exec.Integer()),
			},
			exec.NewComputedOutputType(listElementOutputType),
			listElementExec,
			nil)
		kernel.NullHandling = exec.NullComputedNoPrealloc
		kernel.MemAlloc = exec.MemNoPrealloc
		kernels = append(kernels, kernel)
	}
	return kernels
}
