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

package compute

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/variant"
	"github.com/google/uuid"
)

// VariantGetOptions controls VariantGet.
type VariantGetOptions struct {
	// Path is the path to extract from every variant value.
	Path variant.VariantPath
	// AsType, when nil, makes VariantGet return a VariantArray pointing at the path;
	// when set, the extracted values are cast to it via the cast kernels.
	AsType arrow.DataType
	// Strict makes a lossy cast fail; otherwise a value that cannot convert to AsType
	// nulls only that row, not the rest of its natural-type group.
	Strict bool
}

// VariantGet extracts opts.Path from every value of input. It follows the shredded
// typed_value columns as far as the path allows - stepping into struct fields
// directly and gathering list elements with the take kernel - then reassembles only
// the residual for any remaining path. With AsType nil it returns a VariantArray of
// the extracted values; otherwise it casts them to AsType with the cast kernels.
func VariantGet(ctx context.Context, input *extensions.VariantArray, opts VariantGetOptions) (arrow.Array, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: VariantGet requires a non-nil VariantArray", arrow.ErrInvalid)
	}

	// Reject nested target storage. Unwrap extension types first: they embed
	// ExtensionBase and so satisfy arrow.NestedType even when backed by e.g. UUID.
	nestedCheck := opts.AsType
	if ext, ok := nestedCheck.(arrow.ExtensionType); ok {
		nestedCheck = ext.StorageType()
	}
	if _, ok := nestedCheck.(arrow.NestedType); ok {
		return nil, fmt.Errorf("%w: VariantGet cast to nested type %s", arrow.ErrNotImplemented, opts.AsType)
	}

	// Empty path, no cast: the values are returned unchanged.
	if opts.Path.Len() == 0 && opts.AsType == nil {
		input.Retain()

		return input, nil
	}

	return shreddedGetPath(ctx, input, opts)
}

// shreddingState is a (value?, typed_value?) column pair at one level of a shredded
// variant, mirroring arrow-rs ShreddingState.
type shreddingState struct {
	value      arrow.TypedArray[[]byte]
	typedValue arrow.Array
	length     int
}

func stateFromInput(input *extensions.VariantArray) shreddingState {
	return shreddingState{
		value:      input.UntypedValues(),
		typedValue: input.Shredded(),
		length:     input.Len(),
	}
}

func stateFromFieldStruct(child *array.Struct) shreddingState {
	ct := child.DataType().(*arrow.StructType)

	var value arrow.TypedArray[[]byte]
	if idx, ok := ct.FieldIdx("value"); ok {
		value = child.Field(idx).(arrow.TypedArray[[]byte])
	}

	var typed arrow.Array
	if idx, ok := ct.FieldIdx("typed_value"); ok {
		typed = child.Field(idx)
	}

	return shreddingState{value: value, typedValue: typed, length: child.Len()}
}

type pathStepKind int

const (
	stepSuccess pathStepKind = iota
	stepMissing
)

type pathStep struct {
	kind  pathStepKind
	state shreddingState
	owned []arrow.Array // intermediate take results the caller must release
}

// missingStep marks a path step whose typed field is absent. The descent loop breaks
// on any residual before stepping, so a step that reaches here is provably missing.
func (s shreddingState) missingStep() pathStep {
	return pathStep{kind: stepMissing}
}

// hasResidual reports whether any row carries a value in this level's value column.
func (s shreddingState) hasResidual() bool {
	return s.value != nil && s.value.NullN() != s.value.Len()
}

func fieldStep(s shreddingState, name string) (pathStep, error) {
	if s.typedValue == nil {
		return s.missingStep(), nil
	}
	st, ok := s.typedValue.(*array.Struct)
	if !ok {
		// A field step into a non-object shredded value is a type error, matching the
		// per-row GetByPath path. Any residual was already diverted before this runs.
		return pathStep{}, fmt.Errorf("%w: variant path field %q applied to non-object %s",
			arrow.ErrInvalid, name, s.typedValue.DataType())
	}
	idx, ok := st.DataType().(*arrow.StructType).FieldIdx(name)
	if !ok {
		return s.missingStep(), nil
	}
	child, ok := st.Field(idx).(*array.Struct)
	if !ok {
		return pathStep{}, fmt.Errorf("%w: expected struct field %q while following path, got %s",
			arrow.ErrInvalid, name, st.Field(idx).DataType())
	}

	return pathStep{kind: stepSuccess, state: stateFromFieldStruct(child)}, nil
}

// indexStep gathers element index from every row of a shredded list with the take
// kernel, producing the shredding state one level deeper.
func indexStep(ctx context.Context, mem memory.Allocator, s shreddingState, index int) (pathStep, error) {
	if s.typedValue == nil {
		return s.missingStep(), nil
	}
	list, ok := s.typedValue.(array.ListLike)
	if !ok {
		return s.missingStep(), nil
	}
	elems, ok := list.ListValues().(*array.Struct)
	if !ok {
		return s.missingStep(), nil
	}

	ib := array.NewUint64Builder(mem)
	defer ib.Release()
	ib.Reserve(s.length)
	for row := 0; row < s.length; row++ {
		start, end := list.ValueOffsets(row)
		if list.IsValid(row) && index >= 0 && int64(index) < end-start {
			ib.Append(uint64(start + int64(index)))
		} else {
			ib.AppendNull()
		}
	}
	indices := ib.NewArray()
	defer indices.Release()

	et := elems.DataType().(*arrow.StructType)
	var owned []arrow.Array
	var next shreddingState
	next.length = s.length

	if vi, ok := et.FieldIdx("value"); ok {
		taken, err := TakeArray(ctx, elems.Field(vi), indices)
		if err != nil {
			return pathStep{}, err
		}
		owned = append(owned, taken)
		next.value = taken.(arrow.TypedArray[[]byte])
	}
	if ti, ok := et.FieldIdx("typed_value"); ok {
		taken, err := TakeArray(ctx, elems.Field(ti), indices)
		if err != nil {
			releaseAll(owned)

			return pathStep{}, err
		}
		owned = append(owned, taken)
		next.typedValue = taken
	}

	return pathStep{kind: stepSuccess, state: next, owned: owned}, nil
}

func releaseAll(arrs []arrow.Array) {
	for _, a := range arrs {
		a.Release()
	}
}

func shreddedGetPath(ctx context.Context, input *extensions.VariantArray, opts VariantGetOptions) (arrow.Array, error) {
	mem := GetAllocator(ctx)
	state := stateFromInput(input)
	nulls := newNullTracker(input.Len(), mem)
	defer nulls.release()
	nulls.merge(input.Storage())

	var owned []arrow.Array
	defer func() { releaseAll(owned) }()

	idx := 0
	for idx < opts.Path.Len() {
		// residual-backed rows live in the value column, unreachable by the typed_value descent; reassemble per-row.
		if state.hasResidual() {
			break
		}

		name, index, isField := opts.Path.StepAt(idx)
		var (
			step pathStep
			err  error
		)
		if isField {
			step, err = fieldStep(state, name)
		} else {
			step, err = indexStep(ctx, mem, state, index)
		}
		if err != nil {
			return nil, err
		}

		if step.kind == stepSuccess {
			nulls.merge(state.typedValue)
			state = step.state
			owned = append(owned, step.owned...)
			idx++

			continue
		}

		// stepMissing: the typed field is provably absent (no residual, checked above).
		return allNullResult(mem, input.Len(), opts.AsType), nil
	}

	remaining := subPath(opts.Path, idx)

	// Try to return the typed column directly before building the target array,
	// so a perfect shredding does not allocate a struct and bitmap it discards.
	if remaining.Len() == 0 && opts.AsType != nil {
		if col := perfectShredded(state, nulls, opts.AsType); col != nil {
			defer col.Release()

			return CastArray(ctx, col, NewCastOptions(opts.AsType, opts.Strict))
		}
	}

	target, err := buildTargetVariant(input, state, nulls, mem)
	if err != nil {
		return nil, err
	}
	defer target.Release()

	if remaining.Len() == 0 && opts.AsType == nil {
		target.Retain()

		return target, nil
	}

	leaves, err := extractLeaves(target, remaining)
	if err != nil {
		return nil, err
	}
	if opts.AsType == nil {
		return buildLeafVariantArray(mem, leaves), nil
	}

	return castLeaves(ctx, mem, leaves, opts.AsType, opts.Strict)
}

// perfectShredded returns the typed_value column when the path landed on a fully
// shredded value of exactly AsType and no ancestor nulls need merging; otherwise
// the caller's reassembly path produces the same values.
func perfectShredded(s shreddingState, nulls *nullTracker, asType arrow.DataType) arrow.Array {
	if s.typedValue == nil || !nulls.allValid() {
		return nil
	}
	if !arrow.TypeEqual(s.typedValue.DataType(), asType) {
		return nil
	}
	if s.value != nil && s.value.NullN() != s.value.Len() {
		return nil
	}

	s.typedValue.Retain()

	return s.typedValue
}

func buildTargetVariant(input *extensions.VariantArray, s shreddingState, nulls *nullTracker, mem memory.Allocator) (*extensions.VariantArray, error) {
	// Read the raw metadata column rather than input.Metadata(), which asserts plain
	// binary and panics on dictionary-encoded metadata; the raw column preserves
	// dictionary/large-binary encoding and is decoded by the target's own reader.
	storage := input.Storage().(*array.Struct)
	mdIdx, ok := storage.DataType().(*arrow.StructType).FieldIdx("metadata")
	if !ok {
		return nil, fmt.Errorf("%w: variant storage is missing its metadata field", arrow.ErrInvalid)
	}
	metadata := storage.Field(mdIdx)

	fields := []arrow.Field{{Name: "metadata", Type: metadata.DataType(), Nullable: false}}
	cols := []arrow.Array{metadata}
	if s.value != nil {
		fields = append(fields, arrow.Field{Name: "value", Type: s.value.DataType(), Nullable: true})
		cols = append(cols, s.value)
	}
	if s.typedValue != nil {
		fields = append(fields, arrow.Field{Name: "typed_value", Type: s.typedValue.DataType(), Nullable: true})
		cols = append(cols, s.typedValue)
	}

	bitmap, nullCount := nulls.validityBitmap()
	st, err := array.NewStructArrayWithFieldsAndNulls(cols, fields, bitmap, nullCount, 0)
	if err != nil {
		return nil, err
	}
	defer st.Release()

	vt, err := extensions.NewVariantType(st.DataType())
	if err != nil {
		return nil, err
	}

	return array.NewExtensionArrayWithStorage(vt, st).(*extensions.VariantArray), nil
}

// subPath returns the suffix of p starting at from, rebuilt through the opaque API.
func subPath(p variant.VariantPath, from int) variant.VariantPath {
	var out variant.VariantPath
	for i := from; i < p.Len(); i++ {
		if name, index, isField := p.StepAt(i); isField {
			out = out.Field(name)
		} else {
			out = out.Index(index)
		}
	}

	return out
}

// variantLeaf is one row's extracted value; present is false when the path is
// absent for that row (or the row is null).
type variantLeaf struct {
	value   variant.Value
	present bool
}

func extractLeaves(target *extensions.VariantArray, path variant.VariantPath) ([]variantLeaf, error) {
	leaves := make([]variantLeaf, target.Len())
	for i := range leaves {
		if target.IsNull(i) {
			continue
		}
		v, err := target.Value(i)
		if err != nil {
			return nil, fmt.Errorf("variant: reassembling row %d: %w", i, err)
		}
		leaf, found, err := v.GetByPath(path)
		if err != nil {
			return nil, err
		}
		leaves[i] = variantLeaf{value: leaf, present: found}
	}

	return leaves, nil
}

func buildLeafVariantArray(mem memory.Allocator, leaves []variantLeaf) arrow.Array {
	bldr := extensions.NewVariantBuilder(mem, extensions.NewDefaultVariantType())
	defer bldr.Release()
	bldr.Reserve(len(leaves))
	for _, l := range leaves {
		if !l.present {
			bldr.AppendNull()

			continue
		}
		bldr.Append(l.value)
	}

	return bldr.NewArray()
}

// castLeaves converts each leaf to asType (arrow-rs variant_get parity): leaves are
// grouped by natural type, each group cast with the cast kernels, then scattered back
// so the result is order-independent. Strict errors on a lossy cast, else null.
func castLeaves(ctx context.Context, mem memory.Allocator, leaves []variantLeaf, asType arrow.DataType, strict bool) (arrow.Array, error) {
	type leafGroup struct {
		dt   arrow.DataType
		rows []int
	}
	groups := make(map[string]*leafGroup)
	var order []string
	for i, l := range leaves {
		if !l.present || l.value.Type() == variant.Null {
			continue
		}
		dt := naturalArrowType(l.value)
		if dt == nil {
			// Object/array leaves have no primitive natural type. Under Strict this is an
			// impossible cast (errors like any other); otherwise the row stays null.
			if strict {
				return nil, fmt.Errorf("%w: cannot cast non-primitive variant leaf to %s", arrow.ErrInvalid, asType)
			}

			continue
		}
		key := dt.String()
		g := groups[key]
		if g == nil {
			g = &leafGroup{dt: dt}
			groups[key] = g
			order = append(order, key)
		}
		g.rows = append(g.rows, i)
	}

	perm := make([]uint64, len(leaves))
	valid := make([]bool, len(leaves))
	var casted []arrow.Array
	defer func() { releaseAll(casted) }()

	var pos uint64
	for _, key := range order {
		g := groups[key]
		col := buildTypedColumn(mem, g.dt, leaves, g.rows)
		cast, err := CastArray(ctx, col, NewCastOptions(asType, strict))
		col.Release()
		if err == nil {
			casted = append(casted, cast)
			for _, row := range g.rows {
				perm[row] = pos
				valid[row] = true
				pos++
			}

			continue
		}
		if strict {
			return nil, err
		}
		// Non-strict: retry each row alone so only the inconvertible rows null, not the whole group.
		for _, row := range g.rows {
			rc := buildTypedColumn(mem, g.dt, leaves, []int{row})
			one, cerr := CastArray(ctx, rc, NewCastOptions(asType, strict))
			rc.Release()
			if cerr != nil {
				continue // this row alone is inconvertible; it stays null
			}
			casted = append(casted, one)
			perm[row] = pos
			valid[row] = true
			pos++
		}
	}

	if len(casted) == 0 {
		return allNullResult(mem, len(leaves), asType), nil
	}

	// One natural type covering every row already sits in original order.
	if len(casted) == 1 && pos == uint64(len(leaves)) {
		casted[0].Retain()

		return casted[0], nil
	}

	combined, err := array.Concatenate(casted, mem)
	if err != nil {
		return nil, err
	}
	defer combined.Release()

	ib := array.NewUint64Builder(mem)
	defer ib.Release()
	ib.AppendValues(perm, valid)
	indices := ib.NewArray()
	defer indices.Release()

	return TakeArray(ctx, combined, indices)
}

// buildTypedColumn materializes the given leaf rows, all of natural type dt, into a
// homogeneous Arrow array the cast kernels can consume.
func buildTypedColumn(mem memory.Allocator, dt arrow.DataType, leaves []variantLeaf, rows []int) arrow.Array {
	bldr := array.NewBuilder(mem, dt)
	defer bldr.Release()
	bldr.Reserve(len(rows))
	for _, row := range rows {
		if !appendNatural(bldr, leaves[row].value) {
			bldr.AppendNull()
		}
	}

	return bldr.NewArray()
}

// allNullResult builds the all-null output for a provably missing path.
func allNullResult(mem memory.Allocator, n int, asType arrow.DataType) arrow.Array {
	if asType != nil {
		return array.MakeArrayOfNull(mem, asType, n)
	}

	// MakeArrayOfNull cannot build the variant extension type (its storage struct's
	// metadata/value are non-nullable), so append encoded variant nulls instead.
	bldr := extensions.NewVariantBuilder(mem, extensions.NewDefaultVariantType())
	defer bldr.Release()
	for range n {
		bldr.AppendNull()
	}

	return bldr.NewArray()
}

// nullTracker accumulates ancestor validity bitmaps with a bitmap AND.
type nullTracker struct {
	length int
	mem    memory.Allocator
	buf    *memory.Buffer // validity bitmap (1 = valid); nil means all valid
}

func newNullTracker(length int, mem memory.Allocator) *nullTracker {
	return &nullTracker{length: length, mem: mem}
}

// merge folds arr's validity into the accumulated mask. Arrow validity bits are
// 1=valid, so accumulating ancestor nulls is a bitmap AND (a row is null in the
// result when it is null at any level) - the validity-space equivalent of OR-ing
// null masks.
func (n *nullTracker) merge(arr arrow.Array) {
	if arr == nil {
		return
	}
	vb := arr.Data().Buffers()[0]
	if vb == nil {
		return // all valid
	}
	off := int64(arr.Data().Offset())
	if n.buf == nil {
		n.buf = bitutil.BitmapAndAlloc(n.mem, vb.Bytes(), vb.Bytes(), off, off, int64(n.length), 0)

		return
	}
	merged := bitutil.BitmapAndAlloc(n.mem, n.buf.Bytes(), vb.Bytes(), 0, off, int64(n.length), 0)
	n.buf.Release()
	n.buf = merged
}

func (n *nullTracker) allValid() bool { return n.buf == nil }

func (n *nullTracker) validityBitmap() (*memory.Buffer, int) {
	if n.buf == nil {
		return nil, 0
	}

	return n.buf, n.length - bitutil.CountSetBits(n.buf.Bytes(), 0, n.length)
}

func (n *nullTracker) release() {
	if n.buf != nil {
		n.buf.Release()
		n.buf = nil
	}
}

func naturalArrowType(v variant.Value) arrow.DataType {
	switch v.Type() {
	case variant.Bool:
		return arrow.FixedWidthTypes.Boolean
	case variant.Int8:
		return arrow.PrimitiveTypes.Int8
	case variant.Int16:
		return arrow.PrimitiveTypes.Int16
	case variant.Int32:
		return arrow.PrimitiveTypes.Int32
	case variant.Int64:
		return arrow.PrimitiveTypes.Int64
	case variant.Float:
		return arrow.PrimitiveTypes.Float32
	case variant.Double:
		return arrow.PrimitiveTypes.Float64
	case variant.String:
		return arrow.BinaryTypes.String
	case variant.Binary:
		return arrow.BinaryTypes.Binary
	case variant.Date:
		return arrow.FixedWidthTypes.Date32
	case variant.Time:
		return arrow.FixedWidthTypes.Time64us
	case variant.TimestampMicros:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	case variant.TimestampMicrosNTZ:
		return &arrow.TimestampType{Unit: arrow.Microsecond}
	case variant.TimestampNanos:
		return &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"}
	case variant.TimestampNanosNTZ:
		return &arrow.TimestampType{Unit: arrow.Nanosecond}
	case variant.UUID:
		return extensions.NewUUIDType()
	case variant.Decimal4, variant.Decimal8, variant.Decimal16:
		return &arrow.Decimal128Type{Precision: 38, Scale: int32(decimalScale(v))}
	}

	return nil
}

// appendNatural appends v to bldr when v's value matches bldr's element type,
// reporting whether it did; callers group leaves by natural type first, so it matches.
func appendNatural(bldr array.Builder, v variant.Value) bool {
	switch b := bldr.(type) {
	case *array.BooleanBuilder:
		if x, ok := v.Value().(bool); ok {
			b.Append(x)

			return true
		}
	case *array.Int8Builder:
		if x, ok := v.Value().(int8); ok {
			b.Append(x)

			return true
		}
	case *array.Int16Builder:
		if x, ok := v.Value().(int16); ok {
			b.Append(x)

			return true
		}
	case *array.Int32Builder:
		if x, ok := v.Value().(int32); ok {
			b.Append(x)

			return true
		}
	case *array.Int64Builder:
		if x, ok := v.Value().(int64); ok {
			b.Append(x)

			return true
		}
	case *array.Float32Builder:
		if x, ok := v.Value().(float32); ok {
			b.Append(x)

			return true
		}
	case *array.Float64Builder:
		if x, ok := v.Value().(float64); ok {
			b.Append(x)

			return true
		}
	case *array.StringBuilder:
		if x, ok := v.Value().(string); ok {
			b.Append(x)

			return true
		}
	case *array.BinaryBuilder:
		if x, ok := v.Value().([]byte); ok {
			b.Append(x)

			return true
		}
	case *array.Date32Builder:
		if x, ok := v.Value().(arrow.Date32); ok {
			b.Append(x)

			return true
		}
	case *array.Time64Builder:
		if x, ok := v.Value().(arrow.Time64); ok {
			b.Append(x)

			return true
		}
	case *array.TimestampBuilder:
		if x, ok := v.Value().(arrow.Timestamp); ok {
			b.Append(x)

			return true
		}
	case *extensions.UUIDBuilder:
		if x, ok := v.Value().(uuid.UUID); ok {
			b.Append(x)

			return true
		}
	case *array.Decimal128Builder:
		if num, ok := decimalAsNum128(v); ok && int32(decimalScale(v)) == b.Type().(*arrow.Decimal128Type).Scale {
			b.Append(num)

			return true
		}
	}

	return false
}

func decimalScale(v variant.Value) uint8 {
	switch d := v.Value().(type) {
	case variant.DecimalValue[decimal.Decimal32]:
		return d.Scale
	case variant.DecimalValue[decimal.Decimal64]:
		return d.Scale
	case variant.DecimalValue[decimal.Decimal128]:
		return d.Scale
	}

	return 0
}

func decimalAsNum128(v variant.Value) (decimal128.Num, bool) {
	switch d := v.Value().(type) {
	case variant.DecimalValue[decimal.Decimal32]:
		return decimal128.FromI64(int64(d.Value.(decimal.Decimal32))), true
	case variant.DecimalValue[decimal.Decimal64]:
		return decimal128.FromI64(int64(d.Value.(decimal.Decimal64))), true
	case variant.DecimalValue[decimal.Decimal128]:
		return d.Value.(decimal.Decimal128), true
	}

	return decimal128.Num{}, false
}
