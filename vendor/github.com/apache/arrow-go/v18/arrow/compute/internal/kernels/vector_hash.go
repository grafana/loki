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

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"github.com/apache/arrow-go/v18/arrow/internal/debug"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/apache/arrow-go/v18/internal/bitutils"
	"github.com/apache/arrow-go/v18/internal/hashing"
)

type HashState interface {
	// Reset for another run
	Reset() error
	// Flush out accumulated results from last invocation
	Flush(*exec.ExecResult) error
	// FlushFinal flushes the accumulated results across all invocations
	// of calls. The kernel should not be used again until after
	// Reset() is called.
	FlushFinal(out *exec.ExecResult) error
	// GetDictionary returns the values (keys) accumulated in the dictionary
	// so far.
	GetDictionary() (arrow.ArrayData, error)
	ValueType() arrow.DataType
	// Append prepares the action for the given input (reserving appropriately
	// sized data structures, etc.) and visits the input with the Action
	Append(*exec.KernelCtx, *exec.ArraySpan) error
	Allocator() memory.Allocator
}

type Action interface {
	Reset() error
	Reserve(int) error
	Flush(*exec.ExecResult) error
	FlushFinal(*exec.ExecResult) error
	ObserveFound(int)
	ObserveNotFound(int) error
	ObserveNullFound(int)
	ObserveNullNotFound(int) error
	ShouldEncodeNulls() bool
}

type emptyAction struct {
	mem memory.Allocator
	dt  arrow.DataType
}

func (emptyAction) Reset() error                      { return nil }
func (emptyAction) Reserve(int) error                 { return nil }
func (emptyAction) Flush(*exec.ExecResult) error      { return nil }
func (emptyAction) FlushFinal(*exec.ExecResult) error { return nil }
func (emptyAction) ObserveFound(int)                  {}
func (emptyAction) ObserveNotFound(int) error         { return nil }
func (emptyAction) ObserveNullFound(int)              {}
func (emptyAction) ObserveNullNotFound(int) error     { return nil }
func (emptyAction) ShouldEncodeNulls() bool           { return true }

type uniqueAction = emptyAction

type NullEncodingBehavior int8

const (
	// NullEncodingMask keeps null input values null in the indices array.
	// It is the zero value and default behavior.
	NullEncodingMask NullEncodingBehavior = iota
	// NullEncodingEncode adds null input values to the dictionary as a regular entry.
	NullEncodingEncode
)

type DictionaryEncodeOptions struct {
	// NullEncoding controls how null input values are represented.
	NullEncoding NullEncodingBehavior `compute:"null_encoding_behavior"`
}

func (DictionaryEncodeOptions) TypeName() string { return "DictionaryEncodeOptions" }

func (opts DictionaryEncodeOptions) ToScalar() (scalar.Scalar, error) {
	var encoded uint32
	switch opts.NullEncoding {
	case NullEncodingEncode:
		encoded = 0
	case NullEncodingMask:
		encoded = 1
	default:
		return nil, fmt.Errorf("%w: invalid null encoding behavior %d", arrow.ErrInvalid, opts.NullEncoding)
	}

	return scalar.NewStructScalarWithNames(
		[]scalar.Scalar{
			scalar.NewUint32Scalar(encoded),
			scalar.NewBinaryScalar(memory.NewBufferBytes([]byte(opts.TypeName())), arrow.BinaryTypes.Binary),
		},
		[]string{"null_encoding_behavior", "_type_name"},
	)
}

func (opts *DictionaryEncodeOptions) FromStructScalar(sc *scalar.Struct) error {
	value, err := sc.Field("null_encoding_behavior")
	if err != nil {
		return err
	}

	encoded, ok := value.(*scalar.Uint32)
	if !ok || !encoded.IsValid() {
		return fmt.Errorf("%w: null_encoding_behavior must be a valid uint32 scalar", arrow.ErrInvalid)
	}

	var behavior NullEncodingBehavior
	switch encoded.Value {
	case 0:
		behavior = NullEncodingEncode
	case 1:
		behavior = NullEncodingMask
	default:
		return fmt.Errorf("%w: invalid null encoding behavior %d", arrow.ErrInvalid, encoded.Value)
	}

	opts.NullEncoding = behavior
	return nil
}

type dictionaryEncodeAction struct {
	nullEncoding NullEncodingBehavior
	indices      *bufferBuilder[int32]
	validity     validityBuilder
	length       int
	nulls        int
	err          error
}

func (a *dictionaryEncodeAction) Reset() error {
	a.indices.reset()
	a.validity.reset()
	a.length = 0
	a.nulls = 0
	a.err = nil
	return nil
}

func (a *dictionaryEncodeAction) Reserve(n int) error {
	a.indices.reserve(n)
	a.validity.Reserve(int64(n))
	return nil
}

func (a *dictionaryEncodeAction) appendIndex(idx int, valid bool) {
	if !valid {
		idx = 0
	}
	if idx < 0 || int64(idx) > int64(1<<31-1) {
		if a.err == nil {
			a.err = fmt.Errorf("%w: dictionary index %d does not fit in int32", arrow.ErrInvalid, idx)
		}
		return
	}

	a.indices.unsafeAppend(int32(idx))
	a.validity.UnsafeAppend(valid)
	a.length++
	if !valid {
		a.nulls++
	}
}

func (a *dictionaryEncodeAction) Flush(out *exec.ExecResult) error {
	if a.err != nil {
		return a.err
	}

	out.Len = int64(a.length)
	out.Nulls = int64(a.nulls)
	if a.length != 0 {
		out.Buffers[1].WrapBuffer(a.indices.finish())
	} else {
		a.indices.finish().Release()
	}

	validity := a.validity.Finish()
	if a.nulls != 0 {
		out.Buffers[0].WrapBuffer(validity)
	} else if validity != nil {
		validity.Release()
	}

	a.length = 0
	a.nulls = 0
	return nil
}

func (a *dictionaryEncodeAction) FlushFinal(*exec.ExecResult) error {
	return nil
}

func (a *dictionaryEncodeAction) ObserveFound(idx int) {
	a.appendIndex(idx, true)
}

func (a *dictionaryEncodeAction) ObserveNotFound(idx int) error {
	a.appendIndex(idx, true)
	return a.err
}

func (a *dictionaryEncodeAction) ObserveNullFound(idx int) {
	if a.nullEncoding == NullEncodingMask {
		a.appendIndex(0, false)
	} else {
		a.appendIndex(idx, true)
	}
}

func (a *dictionaryEncodeAction) ObserveNullNotFound(idx int) error {
	a.ObserveNullFound(idx)
	return a.err
}

func (a *dictionaryEncodeAction) ShouldEncodeNulls() bool {
	return a.nullEncoding == NullEncodingEncode
}

type regularHashState struct {
	mem          memory.Allocator
	typ          arrow.DataType
	memoTable    hashing.MemoTable
	action       Action
	memoReleased bool

	doAppend func(Action, hashing.MemoTable, *exec.ArraySpan) error
}

func (rhs *regularHashState) Allocator() memory.Allocator { return rhs.mem }

func (rhs *regularHashState) ValueType() arrow.DataType { return rhs.typ }

func (rhs *regularHashState) Reset() error {
	if rhs.memoReleased {
		memoTable, err := newMemoTable(rhs.mem, rhs.typ.ID())
		if err != nil {
			return err
		}
		rhs.memoTable = memoTable
		rhs.memoReleased = false
	} else {
		rhs.memoTable.Reset()
	}
	return rhs.action.Reset()
}

func (rhs *regularHashState) Append(_ *exec.KernelCtx, arr *exec.ArraySpan) error {
	if err := rhs.action.Reserve(int(arr.Len)); err != nil {
		return err
	}

	return rhs.doAppend(rhs.action, rhs.memoTable, arr)
}

func (rhs *regularHashState) Flush(out *exec.ExecResult) error { return rhs.action.Flush(out) }
func (rhs *regularHashState) FlushFinal(out *exec.ExecResult) error {
	return rhs.action.FlushFinal(out)
}

func (rhs *regularHashState) GetDictionary() (arrow.ArrayData, error) {
	return array.GetDictArrayData(rhs.mem, rhs.typ, rhs.memoTable, 0)
}

func doAppendBinary[OffsetT int32 | int64](action Action, memo hashing.MemoTable, arr *exec.ArraySpan) error {
	if arr.Len == 0 {
		return nil
	}

	var (
		bitmap            = arr.Buffers[0].Buf
		offsets           = exec.GetSpanOffsets[OffsetT](arr, 1)
		data              = arr.Buffers[2].Buf
		shouldEncodeNulls = action.ShouldEncodeNulls()
	)

	return bitutils.VisitBitBlocksShort(bitmap, arr.Offset, arr.Len,
		func(pos int64) error {
			v := data[offsets[pos]:offsets[pos+1]]
			idx, found, err := memo.GetOrInsert(v)
			if err != nil {
				return err
			}
			if found {
				action.ObserveFound(idx)
				return nil
			}
			return action.ObserveNotFound(idx)
		},
		func() error {
			if !shouldEncodeNulls {
				return action.ObserveNullNotFound(-1)
			}

			idx, found := memo.GetOrInsertNull()
			if found {
				action.ObserveNullFound(idx)
				return nil
			}
			return action.ObserveNullNotFound(idx)
		})
}

func doAppendFixedSize(action Action, memo hashing.MemoTable, arr *exec.ArraySpan) error {
	sz := int64(arr.Type.(arrow.FixedWidthDataType).Bytes())
	arrData := arr.Buffers[1].Buf[arr.Offset*sz:]
	shouldEncodeNulls := action.ShouldEncodeNulls()

	return bitutils.VisitBitBlocksShort(arr.Buffers[0].Buf, arr.Offset, arr.Len,
		func(pos int64) error {
			// fixed size type memo table we use a binary memo table
			// so get the raw bytes
			idx, found, err := memo.GetOrInsert(arrData[pos*sz : (pos+1)*sz])
			if err != nil {
				return err
			}
			if found {
				action.ObserveFound(idx)
				return nil
			}
			return action.ObserveNotFound(idx)
		}, func() error {
			if !shouldEncodeNulls {
				return action.ObserveNullNotFound(-1)
			}

			idx, found := memo.GetOrInsertNull()
			if found {
				action.ObserveNullFound(idx)
				return nil
			}
			return action.ObserveNullNotFound(idx)
		})
}

func doAppendNumeric[T uint8 | uint16 | uint32 | uint64](action Action, memo hashing.MemoTable, arr *exec.ArraySpan) error {
	arrData := exec.GetSpanValues[T](arr, 1)
	shouldEncodeNulls := action.ShouldEncodeNulls()
	typedMemo := memo.(hashing.TypedMemoTable[T])
	return bitutils.VisitBitBlocksShort(arr.Buffers[0].Buf, arr.Offset, arr.Len,
		func(pos int64) error {
			idx, found, err := typedMemo.InsertOrGet(arrData[pos])
			if err != nil {
				return err
			}
			if found {
				action.ObserveFound(idx)
				return nil
			}
			return action.ObserveNotFound(idx)
		}, func() error {
			if !shouldEncodeNulls {
				return action.ObserveNullNotFound(-1)
			}

			idx, found := memo.GetOrInsertNull()
			if found {
				action.ObserveNullFound(idx)
				return nil
			}
			return action.ObserveNullNotFound(idx)
		})
}

func doAppendBoolean(action Action, memo hashing.MemoTable, arr *exec.ArraySpan) error {
	if arr.Len == 0 {
		return nil
	}

	values := arr.Buffers[1].Buf
	shouldEncodeNulls := action.ShouldEncodeNulls()
	return bitutils.VisitBitBlocksShort(arr.Buffers[0].Buf, arr.Offset, arr.Len,
		func(pos int64) error {
			value := uint8(0)
			if bitutil.BitIsSet(values, int(arr.Offset+pos)) {
				value = 1
			}
			idx, found, err := memo.GetOrInsert(value)
			if err != nil {
				return err
			}
			if found {
				action.ObserveFound(idx)
				return nil
			}
			return action.ObserveNotFound(idx)
		}, func() error {
			if !shouldEncodeNulls {
				return action.ObserveNullNotFound(-1)
			}

			idx, found := memo.GetOrInsertNull()
			if found {
				action.ObserveNullFound(idx)
				return nil
			}
			return action.ObserveNullNotFound(idx)
		})
}

type nullHashState struct {
	mem      memory.Allocator
	typ      arrow.DataType
	seenNull bool
	action   Action
}

func (nhs *nullHashState) Allocator() memory.Allocator { return nhs.mem }

func (nhs *nullHashState) ValueType() arrow.DataType { return nhs.typ }

func (nhs *nullHashState) Reset() error {
	nhs.seenNull = false
	return nhs.action.Reset()
}

func (nhs *nullHashState) Append(_ *exec.KernelCtx, arr *exec.ArraySpan) (err error) {
	if err := nhs.action.Reserve(int(arr.Len)); err != nil {
		return err
	}

	for i := 0; i < int(arr.Len); i++ {
		if i == 0 {
			nhs.seenNull = true
			err = nhs.action.ObserveNullNotFound(0)
		} else {
			nhs.action.ObserveNullFound(0)
		}
	}
	return
}

func (nhs *nullHashState) Flush(out *exec.ExecResult) error { return nhs.action.Flush(out) }
func (nhs *nullHashState) FlushFinal(out *exec.ExecResult) error {
	return nhs.action.FlushFinal(out)
}

func (nhs *nullHashState) GetDictionary() (arrow.ArrayData, error) {
	var out arrow.Array
	if nhs.seenNull {
		out = array.NewNull(1)
	} else {
		out = array.NewNull(0)
	}
	data := out.Data()
	data.Retain()
	out.Release()
	return data, nil
}

func dictionaryEncodeIdentity(_ *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	data := batch.Values[0].Array.MakeData()
	defer data.Release()
	out.TakeOwnership(data)
	return nil
}

func dictionaryEncodeIdentityChunked(_ *exec.KernelCtx, batch []*arrow.Chunked, _ *exec.ExecResult) ([]*exec.ExecResult, error) {
	if len(batch) != 1 {
		return nil, fmt.Errorf("%w: dictionary_encode expects one input", arrow.ErrInvalid)
	}

	chunks := batch[0].Chunks()
	if len(chunks) == 0 {
		result := &exec.ExecResult{}
		exec.FillZeroLength(batch[0].DataType(), result)
		return []*exec.ExecResult{result}, nil
	}

	results := make([]*exec.ExecResult, 0, len(chunks))
	for _, chunk := range chunks {
		result := &exec.ExecResult{}
		result.TakeOwnership(chunk.Data())
		results = append(results, result)
	}
	return results, nil
}

func initDictionaryEncodeIdentity(_ *exec.KernelCtx, args exec.KernelInitArgs) (exec.KernelState, error) {
	_, err := parseDictionaryEncodeOptions(args.Options)
	return nil, err
}

type dictionaryHashState struct {
	indicesKernel HashState
	dictionary    arrow.Array
	dictValueType arrow.DataType
}

func (dhs *dictionaryHashState) Allocator() memory.Allocator { return dhs.indicesKernel.Allocator() }
func (dhs *dictionaryHashState) Reset() error                { return dhs.indicesKernel.Reset() }
func (dhs *dictionaryHashState) Flush(out *exec.ExecResult) error {
	return dhs.indicesKernel.Flush(out)
}
func (dhs *dictionaryHashState) FlushFinal(out *exec.ExecResult) error {
	return dhs.indicesKernel.FlushFinal(out)
}
func (dhs *dictionaryHashState) GetDictionary() (arrow.ArrayData, error) {
	return dhs.indicesKernel.GetDictionary()
}
func (dhs *dictionaryHashState) ValueType() arrow.DataType           { return dhs.indicesKernel.ValueType() }
func (dhs *dictionaryHashState) DictionaryValueType() arrow.DataType { return dhs.dictValueType }
func (dhs *dictionaryHashState) Dictionary() arrow.Array             { return dhs.dictionary }
func (dhs *dictionaryHashState) Append(ctx *exec.KernelCtx, arr *exec.ArraySpan) error {
	arrDict := arr.Dictionary().MakeArray()
	if dhs.dictionary == nil || array.Equal(dhs.dictionary, arrDict) {
		dhs.dictionary = arrDict
		return dhs.indicesKernel.Append(ctx, arr)
	}

	defer arrDict.Release()

	// NOTE: this approach computes a new dictionary unification per chunk
	// this is in effect O(n*k) where n is the total chunked array length
	// and k is the number of chunks (therefore O(n**2) if chunks have a fixed size).
	//
	// A better approach may be to run the kernel over each individual chunk,
	// and then hash-aggregate all results (for example sum-group-by for
	// the "value_counts" kernel)
	unifier, err := array.NewDictionaryUnifier(dhs.indicesKernel.Allocator(), dhs.dictValueType)
	if err != nil {
		return err
	}
	defer unifier.Release()

	if err := unifier.Unify(dhs.dictionary); err != nil {
		return err
	}
	transposeMap, err := unifier.UnifyAndTranspose(arrDict)
	if err != nil {
		return err
	}
	defer transposeMap.Release()
	_, outDict, err := unifier.GetResult()
	if err != nil {
		return err
	}
	defer func() {
		dhs.dictionary.Release()
		dhs.dictionary = outDict
	}()

	inDict := arr.MakeData()
	defer inDict.Release()
	tmp, err := array.TransposeDictIndices(dhs.Allocator(), inDict, arr.Type, arr.Type, outDict.Data(), arrow.Int32Traits.CastFromBytes(transposeMap.Bytes()))
	if err != nil {
		return err
	}
	defer tmp.Release()

	var tmpSpan exec.ArraySpan
	tmpSpan.SetMembers(tmp)
	return dhs.indicesKernel.Append(ctx, &tmpSpan)
}

func nullHashInit(actionInit initAction) exec.KernelInitFn {
	return func(ctx *exec.KernelCtx, args exec.KernelInitArgs) (exec.KernelState, error) {
		mem := exec.GetAllocator(ctx.Ctx)
		action, err := actionInit(args.Inputs[0], args.Options, mem)
		if err != nil {
			return nil, err
		}
		ret := &nullHashState{
			mem:    mem,
			typ:    args.Inputs[0],
			action: action,
		}
		ret.Reset()
		return ret, nil
	}
}

func newMemoTable(mem memory.Allocator, dt arrow.Type) (hashing.MemoTable, error) {
	switch dt {
	case arrow.BOOL, arrow.INT8, arrow.UINT8:
		return hashing.NewMemoTable[uint8](0), nil
	case arrow.INT16, arrow.UINT16:
		return hashing.NewMemoTable[uint16](0), nil
	case arrow.INT32, arrow.UINT32, arrow.FLOAT32, arrow.DECIMAL32,
		arrow.DATE32, arrow.TIME32, arrow.INTERVAL_MONTHS:
		return hashing.NewMemoTable[uint32](0), nil
	case arrow.INT64, arrow.UINT64, arrow.FLOAT64, arrow.DECIMAL64,
		arrow.DATE64, arrow.TIME64, arrow.TIMESTAMP,
		arrow.DURATION, arrow.INTERVAL_DAY_TIME:
		return hashing.NewMemoTable[uint64](0), nil
	case arrow.BINARY, arrow.STRING, arrow.FIXED_SIZE_BINARY, arrow.DECIMAL128,
		arrow.DECIMAL256, arrow.INTERVAL_MONTH_DAY_NANO:
		return hashing.NewBinaryMemoTable(0, 0,
			array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)), nil
	case arrow.LARGE_BINARY, arrow.LARGE_STRING:
		return hashing.NewBinaryMemoTable(0, 0,
			array.NewBinaryBuilder(mem, arrow.BinaryTypes.LargeBinary)), nil
	default:
		return nil, fmt.Errorf("%w: unsupported type %s", arrow.ErrNotImplemented, dt)
	}
}

func regularHashInit(dt arrow.DataType, actionInit initAction, appendFn func(Action, hashing.MemoTable, *exec.ArraySpan) error) exec.KernelInitFn {
	return func(ctx *exec.KernelCtx, args exec.KernelInitArgs) (exec.KernelState, error) {
		mem := exec.GetAllocator(ctx.Ctx)
		action, err := actionInit(args.Inputs[0], args.Options, mem)
		if err != nil {
			return nil, err
		}
		memoTable, err := newMemoTable(mem, dt.ID())
		if err != nil {
			return nil, err
		}

		ret := &regularHashState{
			mem:       mem,
			typ:       args.Inputs[0],
			memoTable: memoTable,
			action:    action,
			doAppend:  appendFn,
		}
		ret.Reset()
		return ret, nil
	}
}

func dictionaryHashInit(actionInit initAction) exec.KernelInitFn {
	return func(ctx *exec.KernelCtx, args exec.KernelInitArgs) (exec.KernelState, error) {
		var (
			dictType      = args.Inputs[0].(*arrow.DictionaryType)
			indicesHasher exec.KernelState
			err           error
		)

		switch dictType.IndexType.ID() {
		case arrow.INT8, arrow.UINT8:
			indicesHasher, err = getHashInit(arrow.UINT8, actionInit)(ctx, args)
		case arrow.INT16, arrow.UINT16:
			indicesHasher, err = getHashInit(arrow.UINT16, actionInit)(ctx, args)
		case arrow.INT32, arrow.UINT32:
			indicesHasher, err = getHashInit(arrow.UINT32, actionInit)(ctx, args)
		case arrow.INT64, arrow.UINT64:
			indicesHasher, err = getHashInit(arrow.UINT64, actionInit)(ctx, args)
		default:
			return nil, fmt.Errorf("%w: unsupported dictionary index type", arrow.ErrInvalid)
		}
		if err != nil {
			return nil, err
		}

		return &dictionaryHashState{
			indicesKernel: indicesHasher.(HashState),
			dictValueType: dictType.ValueType,
		}, nil
	}
}

type initAction func(arrow.DataType, any, memory.Allocator) (Action, error)

func getHashInit(typeID arrow.Type, actionInit initAction) exec.KernelInitFn {
	switch typeID {
	case arrow.NULL:
		return nullHashInit(actionInit)
	case arrow.BOOL:
		return regularHashInit(arrow.FixedWidthTypes.Boolean, actionInit, doAppendBoolean)
	case arrow.INT8, arrow.UINT8:
		return regularHashInit(arrow.PrimitiveTypes.Uint8, actionInit, doAppendNumeric[uint8])
	case arrow.INT16, arrow.UINT16:
		return regularHashInit(arrow.PrimitiveTypes.Uint16, actionInit, doAppendNumeric[uint16])
	case arrow.INT32, arrow.UINT32, arrow.FLOAT32,
		arrow.DATE32, arrow.TIME32, arrow.INTERVAL_MONTHS:
		return regularHashInit(arrow.PrimitiveTypes.Uint32, actionInit, doAppendNumeric[uint32])
	case arrow.INT64, arrow.UINT64, arrow.FLOAT64,
		arrow.DATE64, arrow.TIME64, arrow.TIMESTAMP,
		arrow.DURATION, arrow.INTERVAL_DAY_TIME:
		return regularHashInit(arrow.PrimitiveTypes.Uint64, actionInit, doAppendNumeric[uint64])
	case arrow.BINARY, arrow.STRING:
		return regularHashInit(arrow.BinaryTypes.Binary, actionInit, doAppendBinary[int32])
	case arrow.LARGE_BINARY, arrow.LARGE_STRING:
		return regularHashInit(arrow.BinaryTypes.LargeBinary, actionInit, doAppendBinary[int64])
	case arrow.FIXED_SIZE_BINARY, arrow.DECIMAL128, arrow.DECIMAL256:
		return regularHashInit(arrow.BinaryTypes.Binary, actionInit, doAppendFixedSize)
	case arrow.INTERVAL_MONTH_DAY_NANO:
		return regularHashInit(arrow.FixedWidthTypes.MonthDayNanoInterval, actionInit, doAppendFixedSize)
	default:
		debug.Assert(false, "unsupported hash init type")
		return nil
	}
}

func hashExec(ctx *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	impl, ok := ctx.State.(HashState)
	if !ok {
		return fmt.Errorf("%w: bad initialization of hash state", arrow.ErrInvalid)
	}

	if err := impl.Append(ctx, &batch.Values[0].Array); err != nil {
		return err
	}

	return impl.Flush(out)
}

func uniqueFinalize(ctx *exec.KernelCtx, results []*exec.ArraySpan) ([]*exec.ArraySpan, error) {
	impl, ok := ctx.State.(HashState)
	if !ok {
		return nil, fmt.Errorf("%w: HashState in invalid state", arrow.ErrInvalid)
	}

	for _, r := range results {
		// release any pre-allocation we did
		r.Release()
	}

	uniques, err := impl.GetDictionary()
	if err != nil {
		return nil, err
	}
	defer uniques.Release()

	var out exec.ArraySpan
	out.TakeOwnership(uniques)
	return []*exec.ArraySpan{&out}, nil
}

func ensureHashDictionary(_ *exec.KernelCtx, hash *dictionaryHashState) (*exec.ArraySpan, error) {
	out := &exec.ArraySpan{}

	if hash.dictionary != nil {
		out.TakeOwnership(hash.dictionary.Data())
		hash.dictionary.Release()
		return out, nil
	}

	exec.FillZeroLength(hash.DictionaryValueType(), out)
	return out, nil
}

func uniqueFinalizeDictionary(ctx *exec.KernelCtx, result []*exec.ArraySpan) (out []*exec.ArraySpan, err error) {
	if out, err = uniqueFinalize(ctx, result); err != nil {
		return
	}

	hash, ok := ctx.State.(*dictionaryHashState)
	if !ok {
		return nil, fmt.Errorf("%w: state should be *dictionaryHashState", arrow.ErrInvalid)
	}

	dict, err := ensureHashDictionary(ctx, hash)
	if err != nil {
		return nil, err
	}
	out[0].SetDictionary(dict)
	return
}

func addHashKernels(base exec.VectorKernel, actionInit initAction, outTy exec.OutputType) []exec.VectorKernel {
	kernels := make([]exec.VectorKernel, 0)
	base.Init = getHashInit(arrow.BOOL, actionInit)
	base.Signature = &exec.KernelSignature{
		InputTypes: []exec.InputType{exec.NewExactInput(arrow.FixedWidthTypes.Boolean)},
		OutType:    outTy,
	}
	kernels = append(kernels, base)

	for _, ty := range primitiveTypes {
		base.Init = getHashInit(ty.ID(), actionInit)
		base.Signature = &exec.KernelSignature{
			InputTypes: []exec.InputType{exec.NewExactInput(ty)},
			OutType:    outTy,
		}
		kernels = append(kernels, base)
	}

	parametricTypes := []arrow.Type{arrow.TIME32, arrow.TIME64, arrow.TIMESTAMP,
		arrow.DURATION, arrow.FIXED_SIZE_BINARY, arrow.DECIMAL128, arrow.DECIMAL256,
		arrow.INTERVAL_DAY_TIME, arrow.INTERVAL_MONTHS, arrow.INTERVAL_MONTH_DAY_NANO}
	for _, ty := range parametricTypes {
		base.Init = getHashInit(ty, actionInit)
		base.Signature = &exec.KernelSignature{
			InputTypes: []exec.InputType{exec.NewIDInput(ty)},
			OutType:    outTy,
		}
		kernels = append(kernels, base)
	}

	return kernels
}

func initUnique(dt arrow.DataType, _ any, mem memory.Allocator) (Action, error) {
	return uniqueAction{mem: mem, dt: dt}, nil
}

func parseDictionaryEncodeOptions(options any) (DictionaryEncodeOptions, error) {
	opts := DictionaryEncodeOptions{}
	switch v := options.(type) {
	case nil:
	case DictionaryEncodeOptions:
		opts = v
	case *DictionaryEncodeOptions:
		if v != nil {
			opts = *v
		}
	default:
		return opts, fmt.Errorf("%w: expected DictionaryEncodeOptions, got %T", arrow.ErrInvalid, options)
	}

	if opts.NullEncoding != NullEncodingMask && opts.NullEncoding != NullEncodingEncode {
		return opts, fmt.Errorf("%w: invalid null encoding behavior %d", arrow.ErrInvalid, opts.NullEncoding)
	}
	return opts, nil
}

func initDictionaryEncode(_ arrow.DataType, options any, mem memory.Allocator) (Action, error) {
	opts, err := parseDictionaryEncodeOptions(options)
	if err != nil {
		return nil, err
	}

	return &dictionaryEncodeAction{
		nullEncoding: opts.NullEncoding,
		indices:      newBufferBuilder[int32](mem),
		validity:     validityBuilder{mem: mem},
	}, nil
}

var outputDictionaryType = exec.NewComputedOutputType(func(_ *exec.KernelCtx, args []arrow.DataType) (arrow.DataType, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%w: dictionary_encode expects one input type", arrow.ErrInvalid)
	}
	return &arrow.DictionaryType{
		IndexType: arrow.PrimitiveTypes.Int32,
		ValueType: args[0],
	}, nil
})

func dictionaryEncodeFinalize(ctx *exec.KernelCtx, results []*exec.ArraySpan) ([]*exec.ArraySpan, error) {
	impl, ok := ctx.State.(HashState)
	if !ok {
		return nil, fmt.Errorf("%w: HashState in invalid state", arrow.ErrInvalid)
	}
	defer releaseHashMemo(impl)

	dict, err := impl.GetDictionary()
	if err != nil {
		return nil, err
	}
	defer dict.Release()

	for _, result := range results {
		var dictSpan exec.ArraySpan
		dictSpan.TakeOwnership(dict)
		result.SetDictionary(&dictSpan)
	}
	return results, nil
}

func releaseHashMemo(hash HashState) {
	if state, ok := hash.(*regularHashState); ok {
		if memo, ok := state.memoTable.(*hashing.BinaryMemoTable); ok && !state.memoReleased {
			memo.Release()
			state.memoReleased = true
		}
	}
}

func GetVectorHashKernels() (unique, valueCounts, dictEncode []exec.VectorKernel) {
	var base exec.VectorKernel
	base.ExecFn = hashExec

	// unique
	base.Finalize = uniqueFinalize
	base.OutputChunked = false
	base.CanExecuteChunkWise = true
	unique = addHashKernels(base, initUnique, OutputFirstType)

	// dictionary unique
	base.Init = dictionaryHashInit(initUnique)
	base.Finalize = uniqueFinalizeDictionary
	base.Signature = &exec.KernelSignature{
		InputTypes: []exec.InputType{exec.NewIDInput(arrow.DICTIONARY)},
		OutType:    OutputFirstType,
	}
	unique = append(unique, base)

	// dictionary encode
	base.Finalize = dictionaryEncodeFinalize
	base.OutputChunked = true
	base.NullHandling = exec.NullComputedNoPrealloc
	base.MemAlloc = exec.MemNoPrealloc
	dictEncode = addHashKernels(base, initDictionaryEncode, outputDictionaryType)
	identity := exec.NewVectorKernelWithSig(
		&exec.KernelSignature{
			InputTypes: []exec.InputType{exec.NewIDInput(arrow.DICTIONARY)},
			OutType:    OutputFirstType,
		},
		dictionaryEncodeIdentity,
		initDictionaryEncodeIdentity)
	identity.CanExecuteChunkWise = false
	identity.ExecChunked = dictionaryEncodeIdentityChunked
	dictEncode = append(dictEncode, identity)

	return
}
