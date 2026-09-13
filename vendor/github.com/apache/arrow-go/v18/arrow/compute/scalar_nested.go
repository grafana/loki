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

package compute

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute/internal/kernels"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

var listElementDoc = FunctionDoc{
	Summary: "Compute elements using nested list values and an index",
	Description: "For each list value, return the element at the requested index.\n" +
		"Use an integral scalar for broadcast selection. A one-element array-like\n" +
		"index is accepted only for a matching one-element execution and is not\n" +
		"broadcast over longer input. Supported inputs are List, LargeList,\n" +
		"ListView, LargeListView, and FixedSizeList; Map is not supported.\n" +
		"Null lists and null child values produce null results. A null or negative\n" +
		"index is invalid, as is an index outside any non-null list's length\n" +
		"(including an empty list). StringView, BinaryView, and dictionary-backed\n" +
		"extension children are not supported, including when nested inside\n" +
		"another child type.",
	ArgNames: []string{"lists", "index"},
}

func validateListElementIndex(index Datum) error {
	switch index.Kind() {
	case KindScalar:
		value, ok := index.(*ScalarDatum)
		if !ok || value.Value == nil {
			return fmt.Errorf("%w: list_element requires a scalar index datum", arrow.ErrType)
		}
		return kernels.ValidateListElementScalarIndex(value.Value)
	case KindArray, KindChunked:
		if index.Len() == 0 {
			return fmt.Errorf("%w: list_element index array is empty", arrow.ErrInvalid)
		}
		if index.Len() > 1 {
			return fmt.Errorf("%w: list_element does not support arrays of list indices", arrow.ErrNotImplemented)
		}
	}
	return nil
}

func validateListElementOutputType(lists Datum) error {
	var typ arrow.DataType
	switch lists.Kind() {
	case KindScalar:
		value, ok := lists.(*ScalarDatum)
		if !ok || value.Value == nil {
			return nil
		}
		typ = value.Value.DataType()
	case KindArray, KindChunked:
		value, ok := lists.(ArrayLikeDatum)
		if !ok {
			return nil
		}
		typ = value.Type()
	default:
		return nil
	}

	listType, ok := typ.(arrow.ListLikeType)
	if !ok || kernels.ListElementOutputTypeSupported(listType.Elem()) {
		return nil
	}
	return fmt.Errorf("%w: list_element output type %s is not supported", arrow.ErrNotImplemented, listType.Elem())
}

type listElementFunction struct {
	ScalarFunction
}

func listElementScalarResultSupported(typ arrow.DataType, nullResult bool) bool {
	switch typ.ID() {
	case arrow.BINARY_VIEW, arrow.STRING_VIEW, arrow.LIST_VIEW, arrow.LARGE_LIST_VIEW,
		arrow.DECIMAL32, arrow.DECIMAL64:
		return false
	case arrow.EXTENSION:
		return listElementScalarResultSupported(typ.(arrow.ExtensionType).StorageType(), nullResult)
	case arrow.STRUCT:
		for _, field := range typ.(*arrow.StructType).Fields() {
			if !listElementScalarResultSupported(field.Type, nullResult) {
				return false
			}
		}
	case arrow.SPARSE_UNION:
		if typ.(arrow.UnionType).NumFields() == 0 {
			return false
		}
		for _, field := range typ.(arrow.UnionType).Fields() {
			if !listElementScalarResultSupported(field.Type, nullResult) {
				return false
			}
		}
	case arrow.DENSE_UNION:
		if !nullResult {
			return true
		}
		fields := typ.(arrow.UnionType).Fields()
		return len(fields) != 0 && listElementScalarResultSupported(fields[0].Type, true)
	case arrow.RUN_END_ENCODED:
		return listElementScalarResultSupported(typ.(*arrow.RunEndEncodedType).Encoded(), nullResult)
	}
	return true
}

func listElementScalarArrayValueSupported(values arrow.Array, index int) bool {
	if values.IsNull(index) {
		return listElementScalarResultSupported(values.DataType(), true)
	}

	switch values := values.(type) {
	case *array.BinaryView, *array.StringView, *array.ListView, *array.LargeListView,
		*array.Decimal32, *array.Decimal64:
		return false
	case array.ExtensionArray:
		return listElementScalarArrayValueSupported(values.Storage(), index)
	case *array.Struct:
		for i := 0; i < values.NumField(); i++ {
			if !listElementScalarArrayValueSupported(values.Field(i), index) {
				return false
			}
		}
	case *array.SparseUnion:
		for i := 0; i < values.NumFields(); i++ {
			if !listElementScalarArrayValueSupported(values.Field(i), index) {
				return false
			}
		}
	case *array.DenseUnion:
		child := values.Field(values.ChildID(index))
		if child == nil {
			return false
		}
		offset := values.ValueOffset(index)
		if offset < 0 || int64(offset) >= int64(child.Len()) {
			return false
		}
		return listElementScalarArrayValueSupported(child, int(offset))
	case *array.RunEndEncoded:
		return listElementScalarArrayValueSupported(values.Values(), values.GetPhysicalIndex(index))
	}
	return true
}

// Validate the index before scalar execution splits arguments into spans. The
// index contract is defined by the original Datum, not by the length of an
// execution span.
func (fn *listElementFunction) Execute(ctx context.Context, opts FunctionOptions, args ...Datum) (Datum, error) {
	if err := fn.checkArity(len(args)); err != nil {
		return nil, err
	}
	if err := checkOptions(fn, opts); err != nil {
		return nil, err
	}

	if err := validateListElementIndex(args[1]); err != nil {
		return nil, err
	}
	if err := validateListElementOutputType(args[0]); err != nil {
		return nil, err
	}
	if args[0].Kind() == KindScalar && args[1].Kind() == KindScalar {
		indexDatum, ok := args[1].(*ScalarDatum)
		if !ok {
			return nil, fmt.Errorf("%w: list_element requires a scalar index datum", arrow.ErrType)
		}
		listDatum, ok := args[0].(*ScalarDatum)
		if !ok {
			return nil, fmt.Errorf("%w: list_element requires a list-like input", arrow.ErrType)
		}
		if listDatum.Value == nil {
			return nil, fmt.Errorf("%w: list_element requires a list-like scalar input", arrow.ErrType)
		}

		listType, ok := listDatum.Type().(arrow.ListLikeType)
		if !ok {
			return nil, fmt.Errorf("%w: list_element requires a list-like input", arrow.ErrType)
		}
		listValue, ok := listDatum.Value.(scalar.ListScalar)
		if !ok {
			return nil, fmt.Errorf("%w: list_element requires a list-like scalar input", arrow.ErrType)
		}
		if !listElementScalarResultSupported(listType.Elem(), !listValue.IsValid()) {
			return nil, fmt.Errorf("%w: list_element scalar output type %s is not supported", arrow.ErrNotImplemented, listType.Elem())
		}
		if !listValue.IsValid() {
			if _, err := fn.DispatchExact(listDatum.Type(), indexDatum.Type()); err != nil {
				return nil, err
			}
			if err := context.Cause(ctx); err != nil {
				return nil, err
			}
			// Null scalar construction does not need array concatenation or
			// unboxing, which do not support every nested scalar type.
			return &ScalarDatum{Value: scalar.MakeNullScalar(listType.Elem())}, nil
		}
		if listValue.GetList() != nil {
			index, err := kernels.ListElementScalarIndex(indexDatum.Value)
			if err != nil {
				return nil, err
			}
			values := listValue.GetList()
			if index < uint64(values.Len()) {
				if !listElementScalarArrayValueSupported(values, int(index)) {
					return nil, fmt.Errorf("%w: list_element scalar output value %s is not supported", arrow.ErrNotImplemented, values.DataType())
				}
				if values.IsNull(int(index)) {
					if _, err := fn.DispatchExact(listDatum.Type(), indexDatum.Type()); err != nil {
						return nil, err
					}
					if err := context.Cause(ctx); err != nil {
						return nil, err
					}
					value, err := scalar.GetScalar(values, int(index))
					if err != nil {
						return nil, err
					}
					return &ScalarDatum{Value: value}, nil
				}
			}
		}
	}

	return fn.ScalarFunction.Execute(ctx, opts, args...)
}

func RegisterScalarNested(reg FunctionRegistry) {
	fn := &listElementFunction{ScalarFunction: *NewScalarFunction("list_element", Binary(), listElementDoc)}
	for _, kernel := range kernels.GetListElementKernels() {
		if err := fn.AddKernel(kernel); err != nil {
			panic(err)
		}
	}

	reg.AddFunction(fn, false)
}

func ListElement(ctx context.Context, lists, index Datum) (Datum, error) {
	return CallFunction(ctx, "list_element", nil, lists, index)
}
