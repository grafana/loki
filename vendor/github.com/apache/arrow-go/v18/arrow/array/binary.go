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
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/internal/bitutils"
	"github.com/apache/arrow-go/v18/internal/json"
)

type BinaryLike interface {
	arrow.Array
	ValueLen(int) int
	ValueBytes() []byte
	ValueOffset64(int) int64
}

// A type which represents an immutable sequence of variable-length binary strings.
type Binary struct {
	array
	valueOffsets []int32
	valueBytes   []byte
}

// NewBinaryData constructs a new Binary array from data.
func NewBinaryData(data arrow.ArrayData) *Binary {
	a := &Binary{}
	a.refCount.Add(1)
	a.setData(data.(*Data))
	return a
}

// Value returns the slice at index i. This value should not be mutated.
func (a *Binary) Value(i int) []byte {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	idx := a.data.offset + i
	return a.valueBytes[a.valueOffsets[idx]:a.valueOffsets[idx+1]]
}

// ValueStr returns a copy of the base64-encoded string value or NullValueStr
func (a *Binary) ValueStr(i int) string {
	if a.IsNull(i) {
		return NullValueStr
	}
	return base64.StdEncoding.EncodeToString(a.Value(i))
}

// ValueString returns the string at index i without performing additional allocations.
// The string is only valid for the lifetime of the Binary array.
func (a *Binary) ValueString(i int) string {
	b := a.Value(i)
	return *(*string)(unsafe.Pointer(&b))
}

func (a *Binary) ValueOffset(i int) int {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	return int(a.valueOffsets[a.data.offset+i])
}

func (a *Binary) ValueOffset64(i int) int64 {
	return int64(a.ValueOffset(i))
}

func (a *Binary) ValueLen(i int) int {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	beg := a.data.offset + i
	return int(a.valueOffsets[beg+1] - a.valueOffsets[beg])
}

func (a *Binary) ValueOffsets() []int32 {
	beg := a.data.offset
	end := beg + a.data.length + 1
	return a.valueOffsets[beg:end]
}

func (a *Binary) ValueBytes() []byte {
	beg := a.data.offset
	end := beg + a.data.length
	return a.valueBytes[a.valueOffsets[beg]:a.valueOffsets[end]]
}

func (a *Binary) String() string {
	o := new(strings.Builder)
	o.WriteString("[")
	for i := 0; i < a.Len(); i++ {
		if i > 0 {
			o.WriteString(" ")
		}
		switch {
		case a.IsNull(i):
			o.WriteString(NullValueStr)
		default:
			fmt.Fprintf(o, "%q", a.ValueString(i))
		}
	}
	o.WriteString("]")
	return o.String()
}

func (a *Binary) setData(data *Data) {
	if len(data.buffers) != 3 {
		panic("len(data.buffers) != 3")
	}

	a.array.setData(data)

	if valueData := data.buffers[2]; valueData != nil {
		a.valueBytes = valueData.Bytes()
	}

	if valueOffsets := data.buffers[1]; valueOffsets != nil {
		a.valueOffsets = arrow.Int32Traits.CastFromBytes(valueOffsets.Bytes())
	}

	if a.data.length < 1 {
		return
	}

	expNumOffsets := a.data.offset + a.data.length + 1
	if len(a.valueOffsets) < expNumOffsets {
		panic(fmt.Errorf("arrow/array: binary offset buffer must have at least %d values", expNumOffsets))
	}

	if int(a.valueOffsets[expNumOffsets-1]) > len(a.valueBytes) {
		panic("arrow/array: binary offsets out of bounds of data buffer")
	}
}

func (a *Binary) GetOneForMarshal(i int) interface{} {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

func (a *Binary) ValueAsAny(i int) any {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

func (a *Binary) MarshalJSON() ([]byte, error) {
	vals := make([]interface{}, a.Len())
	for i := 0; i < a.Len(); i++ {
		vals[i] = a.GetOneForMarshal(i)
	}
	// golang marshal standard says that []byte will be marshalled
	// as a base64-encoded string
	return json.Marshal(vals)
}

// Validate performs a basic, O(1) consistency check on the array data.
// It returns an error if:
//   - The offset buffer is too small for the array length and offset
//   - The last offset exceeds the data buffer length
//
// This is useful for detecting corrupted data from untrusted sources (e.g.
// Arrow Flight / Flight SQL servers) before accessing values, which may
// otherwise cause a runtime panic.
func (a *Binary) Validate() error {
	if a.data.length == 0 {
		return nil
	}
	if a.data.buffers[1] == nil {
		return fmt.Errorf("arrow/array: non-empty binary array has no offsets buffer")
	}
	expNumOffsets := a.data.offset + a.data.length + 1
	if len(a.valueOffsets) < expNumOffsets {
		return fmt.Errorf("arrow/array: binary offset buffer must have at least %d values, got %d", expNumOffsets, len(a.valueOffsets))
	}
	firstOffset := int(a.valueOffsets[a.data.offset])
	if firstOffset > len(a.valueBytes) {
		return fmt.Errorf("arrow/array: binary offset %d out of bounds of data buffer (length %d)", firstOffset, len(a.valueBytes))
	}

	lastOffset := int(a.valueOffsets[expNumOffsets-1])
	if lastOffset > len(a.valueBytes) {
		return fmt.Errorf("arrow/array: binary offset %d out of bounds of data buffer (length %d)", lastOffset, len(a.valueBytes))
	}
	return nil
}

// ValidateFull performs a full O(n) consistency check on the array data.
// In addition to the checks performed by Validate, it also verifies that
// all offsets are non-negative and monotonically non-decreasing.
func (a *Binary) ValidateFull() error {
	if err := a.Validate(); err != nil {
		return err
	}
	if a.data.length == 0 {
		return nil
	}
	offsets := a.valueOffsets[a.data.offset : a.data.offset+a.data.length+1]
	if offsets[0] < 0 {
		return fmt.Errorf("arrow/array: binary offset at index %d is negative: %d", a.data.offset, offsets[0])
	}
	for i := 1; i < len(offsets); i++ {
		if offsets[i] < offsets[i-1] {
			return fmt.Errorf("arrow/array: binary offsets are not monotonically non-decreasing at index %d: %d < %d",
				a.data.offset+i, offsets[i], offsets[i-1])
		}
	}
	return nil
}

func arrayEqualBinary(left, right *Binary) bool {
	if useScalarVariableWidthEquality(left) {
		for i := range left.Len() {
			if !left.IsNull(i) && !bytes.Equal(left.Value(i), right.Value(i)) {
				return false
			}
		}
		return true
	}
	return arrayEqualVariableWidth(
		left.valueOffsets, right.valueOffsets,
		left.valueBytes, right.valueBytes,
		left.Offset(), right.Offset(), left.Len(),
		left.NullN(), left.NullBitmapBytes(),
		bytes.Equal,
	)
}

type LargeBinary struct {
	array
	valueOffsets []int64
	valueBytes   []byte
}

func NewLargeBinaryData(data arrow.ArrayData) *LargeBinary {
	a := &LargeBinary{}
	a.refCount.Add(1)
	a.setData(data.(*Data))
	return a
}

func (a *LargeBinary) Value(i int) []byte {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	idx := a.data.offset + i
	return a.valueBytes[a.valueOffsets[idx]:a.valueOffsets[idx+1]]
}

func (a *LargeBinary) ValueStr(i int) string {
	if a.IsNull(i) {
		return NullValueStr
	}
	return base64.StdEncoding.EncodeToString(a.Value(i))
}

func (a *LargeBinary) ValueString(i int) string {
	b := a.Value(i)
	return *(*string)(unsafe.Pointer(&b))
}

func (a *LargeBinary) ValueOffset(i int) int64 {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	return a.valueOffsets[a.data.offset+i]
}

func (a *LargeBinary) ValueOffset64(i int) int64 {
	return a.ValueOffset(i)
}

func (a *LargeBinary) ValueLen(i int) int {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	beg := a.data.offset + i
	return int(a.valueOffsets[beg+1] - a.valueOffsets[beg])
}

func (a *LargeBinary) ValueOffsets() []int64 {
	beg := a.data.offset
	end := beg + a.data.length + 1
	return a.valueOffsets[beg:end]
}

func (a *LargeBinary) ValueBytes() []byte {
	beg := a.data.offset
	end := beg + a.data.length
	return a.valueBytes[a.valueOffsets[beg]:a.valueOffsets[end]]
}

func (a *LargeBinary) String() string {
	var o strings.Builder
	o.WriteString("[")
	for i := 0; i < a.Len(); i++ {
		if i > 0 {
			o.WriteString(" ")
		}
		switch {
		case a.IsNull(i):
			o.WriteString(NullValueStr)
		default:
			fmt.Fprintf(&o, "%q", a.ValueString(i))
		}
	}
	o.WriteString("]")
	return o.String()
}

func (a *LargeBinary) setData(data *Data) {
	if len(data.buffers) != 3 {
		panic("len(data.buffers) != 3")
	}

	a.array.setData(data)

	if valueData := data.buffers[2]; valueData != nil {
		a.valueBytes = valueData.Bytes()
	}

	if valueOffsets := data.buffers[1]; valueOffsets != nil {
		a.valueOffsets = arrow.Int64Traits.CastFromBytes(valueOffsets.Bytes())
	}

	if a.data.length < 1 {
		return
	}

	expNumOffsets := a.data.offset + a.data.length + 1
	if len(a.valueOffsets) < expNumOffsets {
		panic(fmt.Errorf("arrow/array: large binary offset buffer must have at least %d values", expNumOffsets))
	}

	if int(a.valueOffsets[expNumOffsets-1]) > len(a.valueBytes) {
		panic("arrow/array: large binary offsets out of bounds of data buffer")
	}
}

func (a *LargeBinary) GetOneForMarshal(i int) interface{} {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

func (a *LargeBinary) ValueAsAny(i int) any {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

func (a *LargeBinary) MarshalJSON() ([]byte, error) {
	vals := make([]interface{}, a.Len())
	for i := 0; i < a.Len(); i++ {
		vals[i] = a.GetOneForMarshal(i)
	}
	// golang marshal standard says that []byte will be marshalled
	// as a base64-encoded string
	return json.Marshal(vals)
}

// Validate performs a basic, O(1) consistency check on the array data.
// It returns an error if:
//   - The offset buffer is too small for the array length and offset
//   - The last offset exceeds the data buffer length
//
// This is useful for detecting corrupted data from untrusted sources (e.g.
// Arrow Flight / Flight SQL servers) before accessing values, which may
// otherwise cause a runtime panic.
func (a *LargeBinary) Validate() error {
	if a.data.length == 0 {
		return nil
	}
	if a.data.buffers[1] == nil {
		return fmt.Errorf("arrow/array: non-empty large binary array has no offsets buffer")
	}
	expNumOffsets := a.data.offset + a.data.length + 1
	if len(a.valueOffsets) < expNumOffsets {
		return fmt.Errorf("arrow/array: large binary offset buffer must have at least %d values, got %d", expNumOffsets, len(a.valueOffsets))
	}
	firstOffset := int(a.valueOffsets[a.data.offset])
	if firstOffset > len(a.valueBytes) {
		return fmt.Errorf("arrow/array: large binary offset %d out of bounds of data buffer (length %d)", firstOffset, len(a.valueBytes))
	}

	lastOffset := int(a.valueOffsets[expNumOffsets-1])
	if lastOffset > len(a.valueBytes) {
		return fmt.Errorf("arrow/array: large binary offset %d out of bounds of data buffer (length %d)", lastOffset, len(a.valueBytes))
	}
	return nil
}

// ValidateFull performs a full O(n) consistency check on the array data.
// In addition to the checks performed by Validate, it also verifies that
// all offsets are non-negative and monotonically non-decreasing.
func (a *LargeBinary) ValidateFull() error {
	if err := a.Validate(); err != nil {
		return err
	}
	if a.data.length == 0 {
		return nil
	}
	offsets := a.valueOffsets[a.data.offset : a.data.offset+a.data.length+1]
	if offsets[0] < 0 {
		return fmt.Errorf("arrow/array: large binary offset at index %d is negative: %d", a.data.offset, offsets[0])
	}
	for i := 1; i < len(offsets); i++ {
		if offsets[i] < offsets[i-1] {
			return fmt.Errorf("arrow/array: large binary offsets are not monotonically non-decreasing at index %d: %d < %d",
				a.data.offset+i, offsets[i], offsets[i-1])
		}
	}
	return nil
}

func arrayEqualLargeBinary(left, right *LargeBinary) bool {
	if useScalarVariableWidthEquality(left) {
		for i := range left.Len() {
			if !left.IsNull(i) && !bytes.Equal(left.Value(i), right.Value(i)) {
				return false
			}
		}
		return true
	}
	return arrayEqualVariableWidth(
		left.valueOffsets, right.valueOffsets,
		left.valueBytes, right.valueBytes,
		left.Offset(), right.Offset(), left.Len(),
		left.NullN(), left.NullBitmapBytes(),
		bytes.Equal,
	)
}

type binaryOffset interface {
	~int32 | ~int64
}

func useScalarVariableWidthEquality(values arrow.Array) bool {
	if values.NullN() == 0 {
		return false
	}
	if values.Len() <= 64 || len(values.NullBitmapBytes()) == 0 {
		return true
	}

	// Very short validity runs cost more to set up than direct value comparisons.
	// Sample a few runs and retain the scalar path when they average under four values.
	const (
		sampleRuns          = 8
		minAverageRunLength = 4
	)
	runs := bitutils.NewSetBitRunReader(
		values.NullBitmapBytes(), int64(values.Data().Offset()), int64(values.Len()),
	)
	validValues := int64(0)
	for range sampleRuns {
		run := runs.NextRun()
		if run.Length == 0 {
			return false
		}
		validValues += run.Length
	}
	return validValues < sampleRuns*minAverageRunLength
}

func arrayEqualVariableWidth[T binaryOffset, V ~[]byte | ~string](
	leftOffsets, rightOffsets []T,
	leftValues, rightValues V,
	leftOffset, rightOffset, length, nulls int,
	validity []byte,
	equalValues func(V, V) bool,
) bool {
	if length == 0 {
		return true
	}

	// A declared null count may be inconsistent with the validity bitmap.
	// Verify zero-null bitmaps before comparing the whole payload.
	if len(validity) == 0 ||
		(nulls == 0 && bitutil.CountSetBits(validity, leftOffset, length) == length) {
		return arrayEqualVariableWidthRun(
			leftOffsets, rightOffsets,
			leftValues, rightValues,
			leftOffset, rightOffset, length,
			equalValues,
		)
	}

	runs := bitutils.NewSetBitRunReader(validity, int64(leftOffset), int64(length))
	for {
		run := runs.NextRun()
		if run.Length == 0 {
			return true
		}
		if !arrayEqualVariableWidthRun(
			leftOffsets, rightOffsets,
			leftValues, rightValues,
			leftOffset+int(run.Pos), rightOffset+int(run.Pos), int(run.Length),
			equalValues,
		) {
			return false
		}
	}
}

func arrayEqualVariableWidthRun[T binaryOffset, V ~[]byte | ~string](
	leftOffsets, rightOffsets []T,
	leftValues, rightValues V,
	leftOffset, rightOffset, length int,
	equalValues func(V, V) bool,
) bool {
	leftStart, leftEnd := leftOffsets[leftOffset], leftOffsets[leftOffset+length]
	rightStart, rightEnd := rightOffsets[rightOffset], rightOffsets[rightOffset+length]
	if leftEnd-leftStart != rightEnd-rightStart ||
		!equalValues(
			sliceBinaryValues(leftValues, leftStart, leftEnd),
			sliceBinaryValues(rightValues, rightStart, rightEnd),
		) {
		return false
	}
	if length == 1 {
		return true
	}

	for i := range length {
		if leftOffsets[leftOffset+i+1]-leftOffsets[leftOffset+i] !=
			rightOffsets[rightOffset+i+1]-rightOffsets[rightOffset+i] {
			return false
		}
	}
	return true
}

func sliceBinaryValues[T binaryOffset, V ~[]byte | ~string](values V, start, end T) V {
	return values[start:end]
}

type ViewLike interface {
	arrow.Array
	ValueHeader(int) *arrow.ViewHeader
}

type BinaryView struct {
	array
	values      []arrow.ViewHeader
	dataBuffers []*memory.Buffer
}

func NewBinaryViewData(data arrow.ArrayData) *BinaryView {
	a := &BinaryView{}
	a.refCount.Add(1)
	a.setData(data.(*Data))
	return a
}

func (a *BinaryView) setData(data *Data) {
	if len(data.buffers) < 2 {
		panic("len(data.buffers) < 2")
	}
	a.array.setData(data)

	if valueData := data.buffers[1]; valueData != nil {
		a.values = arrow.ViewHeaderTraits.CastFromBytes(valueData.Bytes())
	}

	a.dataBuffers = data.buffers[2:]
}

func (a *BinaryView) ValueHeader(i int) *arrow.ViewHeader {
	if i < 0 || i >= a.data.length {
		panic("arrow/array: index out of range")
	}
	return &a.values[a.data.offset+i]
}

func (a *BinaryView) Value(i int) []byte {
	s := a.ValueHeader(i)
	if s.IsInline() {
		return s.InlineBytes()
	}
	start := s.BufferOffset()
	buf := a.dataBuffers[s.BufferIndex()]
	return buf.Bytes()[start : start+int32(s.Len())]
}

func (a *BinaryView) ValueLen(i int) int {
	s := a.ValueHeader(i)
	return s.Len()
}

func (a *BinaryView) Validate() error {
	return validateViewLayout(a, "binary view")
}

func (a *BinaryView) ValidateFull() error {
	if err := a.Validate(); err != nil {
		return err
	}
	return validateViewValues(a, a.dataBuffers, nil)
}

// ValueString returns the value at index i as a string instead of
// a byte slice, without copying the underlying data.
func (a *BinaryView) ValueString(i int) string {
	b := a.Value(i)
	return *(*string)(unsafe.Pointer(&b))
}

func (a *BinaryView) String() string {
	var o strings.Builder
	o.WriteString("[")
	for i := 0; i < a.Len(); i++ {
		if i > 0 {
			o.WriteString(" ")
		}
		switch {
		case a.IsNull(i):
			o.WriteString(NullValueStr)
		default:
			fmt.Fprintf(&o, "%q", a.ValueString(i))
		}
	}
	o.WriteString("]")
	return o.String()
}

// ValueStr is paired with AppendValueFromString in that it returns
// the value at index i as a string: Semantically this means that for
// a null value it will return the string "(null)", otherwise it will
// return the value as a base64 encoded string suitable for CSV/JSON.
//
// This is always going to be less performant than just using ValueString
// and exists to fulfill the Array interface to provide a method which
// can produce a human readable string for a given index.
func (a *BinaryView) ValueStr(i int) string {
	if a.IsNull(i) {
		return NullValueStr
	}
	return base64.StdEncoding.EncodeToString(a.Value(i))
}

func (a *BinaryView) GetOneForMarshal(i int) interface{} {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

func (a *BinaryView) ValueAsAny(i int) any {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

func (a *BinaryView) MarshalJSON() ([]byte, error) {
	vals := make([]interface{}, a.Len())
	for i := 0; i < a.Len(); i++ {
		vals[i] = a.GetOneForMarshal(i)
	}
	// golang marshal standard says that []byte will be marshalled
	// as a base64-encoded string
	return json.Marshal(vals)
}

func arrayEqualBinaryView(left, right *BinaryView) bool {
	leftBufs, rightBufs := left.dataBuffers, right.dataBuffers
	for i := 0; i < left.Len(); i++ {
		if left.IsNull(i) {
			continue
		}
		if !left.ValueHeader(i).Equals(leftBufs, right.ValueHeader(i), rightBufs) {
			return false
		}
	}
	return true
}

func validateViewLayout(arr ViewLike, kind string) error {
	data := arr.Data().(*Data)
	if data.length == 0 {
		return nil
	}
	if data.buffers[1] == nil {
		return fmt.Errorf("arrow/array: non-empty %s array has no view buffer", kind)
	}

	expNumViews := data.offset + data.length
	if len(data.buffers[1].Bytes())/arrow.ViewHeaderSizeBytes < expNumViews {
		return fmt.Errorf("arrow/array: %s buffer must have at least %d view values", kind, expNumViews)
	}
	return nil
}

func validateViewValues(arr ViewLike, dataBuffers []*memory.Buffer, validateValue func(int, []byte) error) error {
	data := arr.Data().(*Data)
	if data.length == 0 {
		return nil
	}
	rawViews := data.buffers[1].Bytes()
	for i := 0; i < data.length; i++ {
		if arr.IsNull(i) {
			continue
		}

		view := arr.ValueHeader(i)
		if view.Len() < 0 {
			return fmt.Errorf("arrow/array: view at slot %d has negative size %d", i, view.Len())
		}

		if view.IsInline() {
			rawOffset := (data.offset + i) * arrow.ViewHeaderSizeBytes
			raw := rawViews[rawOffset : rawOffset+arrow.ViewHeaderSizeBytes]
			for _, b := range raw[4+view.Len() : arrow.ViewHeaderSizeBytes] {
				if b != 0 {
					return fmt.Errorf("arrow/array: view at slot %d was inline with size %d but its padding bytes were not all zero", i, view.Len())
				}
			}
			if validateValue != nil {
				if err := validateValue(i, view.InlineBytes()); err != nil {
					return err
				}
			}
			continue
		}

		if view.BufferIndex() < 0 {
			return fmt.Errorf("arrow/array: view at slot %d has negative buffer index %d", i, view.BufferIndex())
		}
		if view.BufferOffset() < 0 {
			return fmt.Errorf("arrow/array: view at slot %d has negative offset %d", i, view.BufferOffset())
		}
		if int(view.BufferIndex()) >= len(dataBuffers) {
			return fmt.Errorf("arrow/array: view at slot %d references buffer %d but there are only %d data buffers", i, view.BufferIndex(), len(dataBuffers))
		}

		buf := dataBuffers[view.BufferIndex()]
		if buf == nil {
			return fmt.Errorf("arrow/array: view at slot %d references nil data buffer %d", i, view.BufferIndex())
		}

		offset := int(view.BufferOffset())
		end := offset + view.Len()
		if end > buf.Len() {
			return fmt.Errorf("arrow/array: view at slot %d references range %d-%d of buffer %d but that buffer is only %d bytes long", i, offset, end, view.BufferIndex(), buf.Len())
		}

		value := buf.Bytes()[offset:end]
		prefix := view.Prefix()
		if !bytes.Equal(value[:arrow.ViewPrefixLen], prefix[:]) {
			return fmt.Errorf("arrow/array: view at slot %d has inlined prefix %x but the out-of-line data begins with %x", i, prefix, value[:arrow.ViewPrefixLen])
		}
		if validateValue != nil {
			if err := validateValue(i, value); err != nil {
				return err
			}
		}
	}
	return nil
}

var (
	_ arrow.Array = (*Binary)(nil)
	_ arrow.Array = (*LargeBinary)(nil)
	_ arrow.Array = (*BinaryView)(nil)

	_ BinaryLike = (*Binary)(nil)
	_ BinaryLike = (*LargeBinary)(nil)

	_ arrow.TypedArray[[]byte] = (*Binary)(nil)
	_ arrow.TypedArray[[]byte] = (*LargeBinary)(nil)
	_ arrow.TypedArray[[]byte] = (*BinaryView)(nil)
)
