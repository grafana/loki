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

package array

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Validator is implemented by array types that provide type-specific
// consistency checks. Validate and ValidateFull also validate generic layout
// and nested child data for every array type.
type Validator interface {
	arrow.Array
	// Validate performs a basic O(1) consistency check.
	Validate() error
	// ValidateFull performs a thorough O(n) consistency check.
	ValidateFull() error
}

// Validate performs a basic O(1) consistency check on arr, returning an error
// if the array's internal buffers or nested child data are inconsistent.
//
// Use this to detect corrupted data from untrusted sources such as Arrow Flight
// or Flight SQL servers before accessing values, which may otherwise panic.
func Validate(arr arrow.Array) error {
	return validateArray(arr, false, "")
}

// ValidateFull performs a thorough O(n) consistency check on arr, returning an
// error if the array's internal buffers or nested child data are inconsistent.
//
// Unlike Validate, this checks every element and is therefore O(n). Use this
// when receiving data from untrusted sources where subtle corruption (e.g.
// non-monotonic offsets) may not be detected by Validate alone.
func ValidateFull(arr arrow.Array) error {
	return validateArray(arr, true, "")
}

func validateArray(arr arrow.Array, full bool, path string) error {
	if arr == nil {
		return nil
	}

	data, ok := arr.Data().(*Data)
	if !ok || data == nil {
		return validationError(path, fmt.Errorf("arrow/array: array does not expose internal data"))
	}

	if err := validateArrayData(data); err != nil {
		return validationError(path, err)
	}
	if err := validateArrayStructure(data); err != nil {
		return validationError(path, err)
	}

	if v, ok := arr.(Validator); ok {
		var err error
		if full {
			err = v.ValidateFull()
		} else {
			err = v.Validate()
		}
		if err != nil {
			return validationError(path, err)
		}
	}

	if full && data.DataType().Layout().HasDict {
		dictData := data.dictionary
		if dictData != nil {
			dt := data.DataType().(*arrow.DictionaryType)
			indexData := NewData(dt.IndexType, data.length, data.buffers, nil, data.nulls, data.offset)
			err := checkIndexBounds(indexData, uint64(dictData.Len()))
			indexData.Release()
			if err != nil {
				return validationError(joinValidationPath(path, "dictionary indices"), err)
			}
		}
	}

	if ext, ok := arr.(ExtensionArray); ok {
		return validateArray(ext.Storage(), full, joinValidationPath(path, "storage"))
	}

	for i, childData := range data.Children() {
		child, err := makeArrayFromData(childData)
		if err != nil {
			return validationError(joinValidationPath(path, validationChildPath(data.DataType(), i)), err)
		}

		childPath := joinValidationPath(path, validationChildPath(data.DataType(), i))
		err = validateArray(child, full, childPath)
		child.Release()
		if err != nil {
			return err
		}
	}

	if dictData := data.dictionary; dictData != nil {
		dict, err := makeArrayFromData(dictData)
		if err != nil {
			return validationError(joinValidationPath(path, "dictionary"), err)
		}

		err = validateArray(dict, full, joinValidationPath(path, "dictionary"))
		dict.Release()
		if err != nil {
			return err
		}
	}

	return nil
}

func validateArrayData(data *Data) error {
	if data == nil || data.dtype == nil {
		return fmt.Errorf("arrow/array: array data has no data type")
	}
	if data.offset < 0 {
		return fmt.Errorf("arrow/array: array offset is negative: %d", data.offset)
	}
	if data.length < 0 {
		return fmt.Errorf("arrow/array: array length is negative: %d", data.length)
	}
	if data.nulls < UnknownNullCount || data.nulls > data.length {
		return fmt.Errorf("arrow/array: invalid null count %d for length %d", data.nulls, data.length)
	}

	end := int64(data.offset) + int64(data.length)
	if end < int64(data.offset) {
		return fmt.Errorf("arrow/array: array offset and length overflow")
	}

	layout := data.dtype.Layout()
	// Union arrays reserve the first buffer slot for a validity bitmap, even
	// though that slot must be nil and is not part of their type layout.
	bufferOffset := 0
	if data.dtype.ID() == arrow.SPARSE_UNION || data.dtype.ID() == arrow.DENSE_UNION {
		bufferOffset = 1
	}
	bufferCount := len(data.buffers) - bufferOffset
	if bufferCount < 0 {
		bufferCount = 0
	}
	if bufferCount < len(layout.Buffers) {
		return fmt.Errorf("arrow/array: expected at least %d buffers for %s, got %d",
			len(layout.Buffers), data.dtype, bufferCount)
	}
	if layout.VariadicSpec == nil && bufferCount > len(layout.Buffers) {
		return fmt.Errorf("arrow/array: expected at most %d buffers for %s, got %d",
			len(layout.Buffers), data.dtype, bufferCount)
	}

	for i, spec := range layout.Buffers {
		if err := validateBuffer(data.buffers[i+bufferOffset], spec, end, data.length, i+bufferOffset); err != nil {
			return err
		}
	}
	if layout.VariadicSpec != nil {
		for i := len(layout.Buffers); i < bufferCount; i++ {
			if err := validateBuffer(data.buffers[i+bufferOffset], *layout.VariadicSpec, end, data.length, i+bufferOffset); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBuffer(buf *memory.Buffer, spec arrow.BufferSpec, end int64, length, index int) error {
	if buf == nil {
		if spec.Kind == arrow.KindFixedWidth {
			if length > 0 {
				return fmt.Errorf("arrow/array: buffer %d is nil for non-empty layout kind %v", index, spec.Kind)
			}
		}
		return nil
	}

	bufferLen := int64(buf.Len())
	switch spec.Kind {
	case arrow.KindFixedWidth:
		if spec.ByteWidth <= 0 {
			return fmt.Errorf("arrow/array: buffer %d has invalid fixed width %d", index, spec.ByteWidth)
		}
		if end > bufferLen/int64(spec.ByteWidth) {
			return fmt.Errorf("arrow/array: buffer %d is too small for offset %d and length %d", index, end-int64(length), length)
		}
	case arrow.KindBitmap:
		if int64(bitutil.BytesForBits(end)) > bufferLen {
			return fmt.Errorf("arrow/array: bitmap buffer %d is too small for offset %d and length %d", index, end-int64(length), length)
		}
	case arrow.KindVarWidth:
		return nil
	case arrow.KindAlwaysNull:
		return fmt.Errorf("arrow/array: buffer %d must be nil for an always-null layout", index)
	}
	return nil
}

func validateArrayStructure(data *Data) error {
	children := data.Children()
	if nested, ok := data.DataType().(arrow.NestedType); ok {
		fields := nested.Fields()
		if len(children) != len(fields) {
			return fmt.Errorf("arrow/array: %s expects %d child arrays, got %d", data.DataType(), len(fields), len(children))
		}
		for i, child := range children {
			if child == nil {
				return fmt.Errorf("arrow/array: child %d of %s is nil", i, data.DataType())
			}
			if !arrow.TypeEqual(fields[i].Type, child.DataType()) {
				return fmt.Errorf("arrow/array: child %d of %s has type %s, want %s",
					i, data.DataType(), child.DataType(), fields[i].Type)
			}
		}
	} else if len(children) != 0 {
		return fmt.Errorf("arrow/array: %s must not have child arrays", data.DataType())
	}

	if data.DataType().Layout().HasDict {
		dt, ok := data.DataType().(*arrow.DictionaryType)
		if !ok {
			return fmt.Errorf("arrow/array: datatype %s declares dictionary storage but is not a dictionary type", data.DataType())
		}
		if dict := data.dictionary; dict != nil && !arrow.TypeEqual(dt.ValueType, dict.DataType()) {
			return fmt.Errorf("arrow/array: dictionary has type %s, want %s", dict.DataType(), dt.ValueType)
		}
		if data.Len() > 0 && data.dictionary == nil {
			return fmt.Errorf("arrow/array: non-empty dictionary array has no dictionary")
		}
	}
	return nil
}

func validateListArray(a *List, full bool) error {
	if err := validateArrayData(a.data); err != nil {
		return err
	}
	if len(a.data.childData) != 1 || a.data.childData[0] == nil {
		return fmt.Errorf("arrow/array: list array must have one non-nil child array")
	}
	// MAP uses the list-like entry layout; large-list variants have dedicated validators.
	dt, ok := a.data.dtype.(arrow.ListLikeType)
	if !ok || dt.ID() == arrow.LARGE_LIST || dt.ID() == arrow.LARGE_LIST_VIEW {
		return fmt.Errorf("arrow/array: invalid datatype %s for list array", a.data.dtype)
	}
	if !arrow.TypeEqual(dt.Elem(), a.data.childData[0].DataType()) {
		return fmt.Errorf("arrow/array: list values have type %s, want %s", a.data.childData[0].DataType(), dt.Elem())
	}
	if len(a.offsets) == 0 && a.data.length > 0 {
		return fmt.Errorf("arrow/array: non-empty list array has no offsets")
	}
	return validateListOffsets(a.offsets, a.data, a.data.childData[0].Len(), full)
}

func validateLargeListArray(a *LargeList, full bool) error {
	if err := validateArrayData(a.data); err != nil {
		return err
	}
	if len(a.data.childData) != 1 || a.data.childData[0] == nil {
		return fmt.Errorf("arrow/array: large list array must have one non-nil child array")
	}
	dt, ok := a.data.dtype.(arrow.ListLikeType)
	if !ok || dt.ID() != arrow.LARGE_LIST {
		return fmt.Errorf("arrow/array: invalid datatype %s for large list array", a.data.dtype)
	}
	if !arrow.TypeEqual(dt.Elem(), a.data.childData[0].DataType()) {
		return fmt.Errorf("arrow/array: large list values have type %s, want %s", a.data.childData[0].DataType(), dt.Elem())
	}
	if len(a.offsets) == 0 && a.data.length > 0 {
		return fmt.Errorf("arrow/array: non-empty large list array has no offsets")
	}
	return validateListOffsets(a.offsets, a.data, a.data.childData[0].Len(), full)
}

func validateListOffsets(offsets interface{}, data *Data, valueLength int, full bool) error {
	if data.length == 0 {
		return nil
	}
	start := int64(data.offset)
	required := start + int64(data.length) + 1
	if required < start || required > int64(offsetLength(offsets)) {
		return fmt.Errorf("arrow/array: list offsets buffer is too small for offset %d and length %d", data.offset, data.length)
	}
	get := func(i int64) int64 {
		switch v := offsets.(type) {
		case []int32:
			return int64(v[int(i)])
		case []int64:
			return v[int(i)]
		default:
			return 0
		}
	}

	first := get(start)
	last := get(required - 1)
	if err := validateListOffset(first, start, valueLength); err != nil {
		return err
	}
	if err := validateListOffset(last, required-1, valueLength); err != nil {
		return err
	}
	if !full {
		return nil
	}

	previous := first
	for i := start + 1; i < required; i++ {
		current := get(i)
		if current < 0 || current > int64(valueLength) {
			return fmt.Errorf("arrow/array: list offset at index %d out of bounds: %d", i, current)
		}
		if current < previous {
			return fmt.Errorf("arrow/array: list offsets are not monotonically non-decreasing at index %d: %d < %d",
				i, current, previous)
		}
		previous = current
	}
	return nil
}

func offsetLength(offsets interface{}) int {
	switch v := offsets.(type) {
	case []int32:
		return len(v)
	case []int64:
		return len(v)
	default:
		return 0
	}
}

func validateListOffset(offset int64, index int64, valueLength int) error {
	if offset < 0 {
		return fmt.Errorf("arrow/array: list offset at index %d is negative: %d", index, offset)
	}
	if offset > int64(valueLength) {
		return fmt.Errorf("arrow/array: list offset at index %d out of bounds: %d > %d", index, offset, valueLength)
	}
	return nil
}

func validateFixedSizeListArray(a *FixedSizeList) error {
	if err := validateArrayData(a.data); err != nil {
		return err
	}
	if len(a.data.childData) != 1 || a.data.childData[0] == nil {
		return fmt.Errorf("arrow/array: fixed-size list array must have one non-nil child array")
	}
	dt, ok := a.data.dtype.(*arrow.FixedSizeListType)
	if !ok {
		return fmt.Errorf("arrow/array: invalid datatype %s for fixed-size list array", a.data.dtype)
	}
	if !arrow.TypeEqual(dt.Elem(), a.data.childData[0].DataType()) {
		return fmt.Errorf("arrow/array: fixed-size list values have type %s, want %s", a.data.childData[0].DataType(), dt.Elem())
	}
	childLength := int64(a.data.offset) + int64(a.data.length)
	itemCount := int64(dt.Len())
	if itemCount <= 0 {
		return fmt.Errorf("arrow/array: fixed-size list has invalid item count %d", itemCount)
	}
	if childLength > int64(a.data.childData[0].Len())/itemCount {
		return fmt.Errorf("arrow/array: fixed-size list child length %d is too small for offset %d and length %d",
			a.data.childData[0].Len(), a.data.offset, a.data.length)
	}
	return nil
}

func makeArrayFromData(data arrow.ArrayData) (arr arrow.Array, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("arrow/array: failed to construct child array: %v", r)
		}
	}()
	return MakeFromData(data), nil
}

func validationChildPath(dt arrow.DataType, index int) string {
	if nested, ok := dt.(arrow.NestedType); ok {
		fields := nested.Fields()
		if index >= 0 && index < len(fields) {
			field := fields[index]
			if dt.ID() == arrow.STRUCT || dt.ID() == arrow.SPARSE_UNION || dt.ID() == arrow.DENSE_UNION || dt.ID() == arrow.RUN_END_ENCODED {
				return fmt.Sprintf("field %q", field.Name)
			}
		}
	}

	switch dt.ID() {
	case arrow.LIST, arrow.LARGE_LIST, arrow.FIXED_SIZE_LIST:
		return "list values"
	case arrow.LIST_VIEW, arrow.LARGE_LIST_VIEW:
		return "list view values"
	case arrow.MAP:
		return "map entries"
	default:
		return fmt.Sprintf("child %d", index)
	}
}

func joinValidationPath(path, child string) string {
	if path == "" {
		return child
	}
	return path + " -> " + child
}

func validationError(path string, err error) error {
	if err == nil || path == "" {
		return err
	}
	return fmt.Errorf("%s: %w", path, err)
}

// ValidateRecord validates each column in rec using Validate, returning the
// first error encountered. The error includes the column index and field name.
func ValidateRecord(rec arrow.RecordBatch) error {
	for i := int64(0); i < rec.NumCols(); i++ {
		if err := Validate(rec.Column(int(i))); err != nil {
			return fmt.Errorf("column %d (%s): %w", i, rec.Schema().Field(int(i)).Name, err)
		}
	}
	return nil
}

// ValidateRecordFull validates each column in rec using ValidateFull, returning
// the first error encountered. The error includes the column index and field name.
func ValidateRecordFull(rec arrow.RecordBatch) error {
	for i := int64(0); i < rec.NumCols(); i++ {
		if err := ValidateFull(rec.Column(int(i))); err != nil {
			return fmt.Errorf("column %d (%s): %w", i, rec.Schema().Field(int(i)).Name, err)
		}
	}
	return nil
}
