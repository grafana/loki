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
	"reflect"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

// CumulativeOptions controls cumulative operations.
type CumulativeOptions struct {
	// Start is the initial value. A nil value uses the zero value for the
	// input type. A non-nil null scalar is invalid.
	Start scalar.Scalar `compute:"start"`
	// SkipNulls controls whether a null input stops the cumulative operation.
	// Null positions are still null in the output when this is true.
	SkipNulls bool `compute:"skip_nulls"`
}

func (CumulativeOptions) TypeName() string { return "CumulativeOptions" }

func (opts CumulativeOptions) Release() {
	if isNilScalar(opts.Start) {
		return
	}

	if releasable, ok := opts.Start.(interface{ Release() }); ok {
		releasable.Release()
	}
}

type cumulativeSumState[T arrow.NumericType] struct {
	current         T
	skipNulls       bool
	encounteredNull bool
	checked         bool
	add             func(T, T) (T, error)
}

type ScalarCastFn func(*exec.KernelCtx, scalar.Scalar, arrow.DataType) (scalar.Scalar, error)

func isNilScalar(value scalar.Scalar) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Ptr && reflected.IsNil()
}

func safeNumericCastScalar(ctx *exec.KernelCtx, cast ScalarCastFn, start scalar.Scalar, typ arrow.DataType) (scalar.Scalar, error) {
	targetID := typ.ID()
	if !arrow.IsInteger(targetID) && !arrow.IsFloating(targetID) {
		return nil, fmt.Errorf("%w: cumulative sum input type must be numeric, got %s", arrow.ErrType, typ)
	}
	if arrow.TypeEqual(start.DataType(), typ) {
		return start, nil
	}

	if cast == nil {
		return nil, fmt.Errorf("%w: cumulative sum start value caster is not configured", arrow.ErrInvalid)
	}

	casted, err := cast(ctx, start, typ)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot cast cumulative sum start value to %s: %v", arrow.ErrInvalid, typ, err)
	}
	return casted, nil
}

func cumulativeStartValue[T arrow.NumericType](ctx *exec.KernelCtx, cast ScalarCastFn, start scalar.Scalar, typ arrow.DataType) (T, error) {
	var zero T
	if isNilScalar(start) {
		return zero, nil
	}
	if !start.IsValid() {
		return zero, fmt.Errorf("%w: cumulative sum start value must be valid", arrow.ErrInvalid)
	}

	casted, err := safeNumericCastScalar(ctx, cast, start, typ)
	if err != nil {
		return zero, err
	}
	if releasable, ok := casted.(scalar.Releasable); ok {
		defer releasable.Release()
	}

	primitive, ok := casted.(scalar.PrimitiveScalar)
	if !ok {
		return zero, fmt.Errorf("%w: cumulative sum start value is not primitive", arrow.ErrInvalid)
	}

	span := &exec.ArraySpan{Type: typ, Len: 1}
	span.Buffers[1].Buf = primitive.Data()
	return exec.GetSpanValues[T](span, 1)[0], nil
}

func initCumulativeSum[T arrow.NumericType](checked bool, cast ScalarCastFn) exec.KernelInitFn {
	return func(ctx *exec.KernelCtx, args exec.KernelInitArgs) (exec.KernelState, error) {
		opts := CumulativeOptions{}
		switch value := args.Options.(type) {
		case nil:
		case CumulativeOptions:
			opts = value
		case *CumulativeOptions:
			if value != nil {
				opts = *value
			}
		default:
			return nil, fmt.Errorf("%w: attempted to initialize cumulative sum from invalid function options", arrow.ErrInvalid)
		}

		start, err := cumulativeStartValue[T](ctx, cast, opts.Start, args.Inputs[0])
		if err != nil {
			return nil, err
		}

		return &cumulativeSumState[T]{
			current:   start,
			skipNulls: opts.SkipNulls,
			checked:   checked,
			add:       checkedAdder[T](),
		}, nil
	}
}

func checkedAddSigned[T arrow.IntType](left, right T) (T, error) {
	if (right > 0 && left > MaxOf[T]()-right) || (right < 0 && left < MinOf[T]()-right) {
		return 0, errOverflow
	}
	return left + right, nil
}

func checkedAddUnsigned[T arrow.UintType](left, right T) (T, error) {
	if left > MaxOf[T]()-right {
		return 0, errOverflow
	}
	return left + right, nil
}

func checkedAdder[T arrow.NumericType]() func(T, T) (T, error) {
	var zero T
	switch any(zero).(type) {
	case int8:
		return func(left, right T) (T, error) {
			value, err := checkedAddSigned(int8(left), int8(right))
			return T(value), err
		}
	case int16:
		return func(left, right T) (T, error) {
			value, err := checkedAddSigned(int16(left), int16(right))
			return T(value), err
		}
	case int32:
		return func(left, right T) (T, error) {
			value, err := checkedAddSigned(int32(left), int32(right))
			return T(value), err
		}
	case int64:
		return func(left, right T) (T, error) {
			value, err := checkedAddSigned(int64(left), int64(right))
			return T(value), err
		}
	case uint8:
		return func(left, right T) (T, error) {
			value, err := checkedAddUnsigned(uint8(left), uint8(right))
			return T(value), err
		}
	case uint16:
		return func(left, right T) (T, error) {
			value, err := checkedAddUnsigned(uint16(left), uint16(right))
			return T(value), err
		}
	case uint32:
		return func(left, right T) (T, error) {
			value, err := checkedAddUnsigned(uint32(left), uint32(right))
			return T(value), err
		}
	case uint64:
		return func(left, right T) (T, error) {
			value, err := checkedAddUnsigned(uint64(left), uint64(right))
			return T(value), err
		}
	default:
		return func(left, right T) (T, error) { return left + right, nil }
	}
}

func prepareCumulativeOutput[T arrow.NumericType](ctx *exec.KernelCtx, out *exec.ExecResult, needsValidity bool) {
	if out.Len == 0 {
		return
	}

	data := ctx.Allocate(int(out.Len) * arrow.GetDataType[T]().(arrow.FixedWidthDataType).Bytes())
	out.Buffers[1].WrapBuffer(data)

	if needsValidity {
		validity := ctx.AllocateBitmap(out.Len)
		validityBytes := validity.Bytes()
		for i := range validityBytes {
			validityBytes[i] = 0xFF
		}
		out.Buffers[0].WrapBuffer(validity)
	}
}

func cumulativeSumNoNulls[T arrow.NumericType](state *cumulativeSumState[T], inputs []*exec.ArraySpan, values []T) {
	var outputOffset int64
	current := state.current
	for _, input := range inputs {
		inputValues := exec.GetSpanValues[T](input, 1)
		for i := int64(0); i < input.Len; i++ {
			current += inputValues[i]
			values[outputOffset+i] = current
		}
		outputOffset += input.Len
	}
	state.current = current
}

func cumulativeSumNoNullsChecked[T arrow.NumericType](state *cumulativeSumState[T], inputs []*exec.ArraySpan, values []T) error {
	var outputOffset int64
	current := state.current
	for _, input := range inputs {
		inputValues := exec.GetSpanValues[T](input, 1)
		for i := int64(0); i < input.Len; i++ {
			var err error
			current, err = state.add(current, inputValues[i])
			if err != nil {
				return err
			}
			values[outputOffset+i] = current
		}
		outputOffset += input.Len
	}
	state.current = current
	return nil
}

func cumulativeSumWithNulls[T arrow.NumericType](state *cumulativeSumState[T], inputs []*exec.ArraySpan, values []T, validity []byte) int64 {
	var (
		nulls        int64
		outputOffset int64
	)
	current := state.current
	for _, input := range inputs {
		inputValues := exec.GetSpanValues[T](input, 1)
		for i := int64(0); i < input.Len; i++ {
			valid := len(input.Buffers[0].Buf) == 0 || bitutil.BitIsSet(input.Buffers[0].Buf, int(input.Offset+i))
			outputIndex := outputOffset + i
			if !valid || state.encounteredNull {
				bitutil.ClearBit(validity, int(outputIndex))
				nulls++
				if !valid && !state.skipNulls {
					state.encounteredNull = true
				}
				continue
			}

			current += inputValues[i]
			values[outputIndex] = current
		}
		outputOffset += input.Len
	}
	state.current = current
	return nulls
}

func cumulativeSumWithNullsChecked[T arrow.NumericType](state *cumulativeSumState[T], inputs []*exec.ArraySpan, values []T, validity []byte) (int64, error) {
	var (
		nulls        int64
		outputOffset int64
	)
	current := state.current
	for _, input := range inputs {
		inputValues := exec.GetSpanValues[T](input, 1)
		for i := int64(0); i < input.Len; i++ {
			valid := len(input.Buffers[0].Buf) == 0 || bitutil.BitIsSet(input.Buffers[0].Buf, int(input.Offset+i))
			outputIndex := outputOffset + i
			if !valid || state.encounteredNull {
				bitutil.ClearBit(validity, int(outputIndex))
				nulls++
				if !valid && !state.skipNulls {
					state.encounteredNull = true
				}
				continue
			}

			var err error
			current, err = state.add(current, inputValues[i])
			if err != nil {
				return nulls, err
			}
			values[outputIndex] = current
		}
		outputOffset += input.Len
	}
	state.current = current
	return nulls, nil
}

func cumulativeSumSpans[T arrow.NumericType](ctx *exec.KernelCtx, state *cumulativeSumState[T], inputs []*exec.ArraySpan, out *exec.ExecResult, needsValidity bool) error {
	prepareCumulativeOutput[T](ctx, out, needsValidity)
	values := exec.GetSpanValues[T](out, 1)

	if !needsValidity && !state.encounteredNull {
		if state.checked {
			if err := cumulativeSumNoNullsChecked(state, inputs, values); err != nil {
				out.Release()
				return err
			}
		} else {
			cumulativeSumNoNulls(state, inputs, values)
		}
		return nil
	}

	var (
		nulls int64
		err   error
	)
	if state.checked {
		nulls, err = cumulativeSumWithNullsChecked(state, inputs, values, out.Buffers[0].Buf)
	} else {
		nulls = cumulativeSumWithNulls(state, inputs, values, out.Buffers[0].Buf)
	}
	if err != nil {
		out.Release()
		return err
	}
	out.Nulls = nulls
	return nil
}

func cumulativeSumExec[T arrow.NumericType](ctx *exec.KernelCtx, batch *exec.ExecSpan, out *exec.ExecResult) error {
	state := ctx.State.(*cumulativeSumState[T])
	input := &batch.Values[0].Array

	out.Len = input.Len
	if input.Len == 0 {
		return nil
	}

	return cumulativeSumSpans(ctx, state, []*exec.ArraySpan{input}, out, state.encounteredNull || input.MayHaveNulls())
}

func cumulativeSumExecChunked[T arrow.NumericType](ctx *exec.KernelCtx, batch []*arrow.Chunked, out *exec.ExecResult) ([]*exec.ExecResult, error) {
	state := ctx.State.(*cumulativeSumState[T])
	input := batch[0]
	out.Len = int64(input.Len())
	if out.Len == 0 {
		return []*exec.ExecResult{out}, nil
	}

	chunks := input.Chunks()
	spans := make([]exec.ArraySpan, len(chunks))
	inputs := make([]*exec.ArraySpan, len(chunks))
	needsValidity := state.encounteredNull || input.NullN() != 0
	for i, chunk := range chunks {
		spans[i].SetMembers(chunk.Data())
		inputs[i] = &spans[i]
		needsValidity = needsValidity || spans[i].MayHaveNulls()
	}

	if err := cumulativeSumSpans(ctx, state, inputs, out, needsValidity); err != nil {
		return nil, err
	}
	return []*exec.ExecResult{out}, nil
}

func newCumulativeSumKernel[T arrow.NumericType](typ arrow.DataType, checked bool, cast ScalarCastFn) exec.VectorKernel {
	kernel := exec.NewVectorKernel(
		[]exec.InputType{exec.NewExactInput(typ)},
		exec.NewOutputType(typ),
		cumulativeSumExec[T],
		initCumulativeSum[T](checked, cast))
	kernel.Parallelizable = false
	kernel.CanExecuteChunkWise = false
	kernel.ExecChunked = cumulativeSumExecChunked[T]
	return kernel
}

func cumulativeSumKernels(checked bool, cast ScalarCastFn) []exec.VectorKernel {
	return []exec.VectorKernel{
		newCumulativeSumKernel[int8](arrow.PrimitiveTypes.Int8, checked, cast),
		newCumulativeSumKernel[int16](arrow.PrimitiveTypes.Int16, checked, cast),
		newCumulativeSumKernel[int32](arrow.PrimitiveTypes.Int32, checked, cast),
		newCumulativeSumKernel[int64](arrow.PrimitiveTypes.Int64, checked, cast),
		newCumulativeSumKernel[uint8](arrow.PrimitiveTypes.Uint8, checked, cast),
		newCumulativeSumKernel[uint16](arrow.PrimitiveTypes.Uint16, checked, cast),
		newCumulativeSumKernel[uint32](arrow.PrimitiveTypes.Uint32, checked, cast),
		newCumulativeSumKernel[uint64](arrow.PrimitiveTypes.Uint64, checked, cast),
		newCumulativeSumKernel[float32](arrow.PrimitiveTypes.Float32, checked, cast),
		newCumulativeSumKernel[float64](arrow.PrimitiveTypes.Float64, checked, cast),
	}
}

func GetVectorCumulativeKernels(cast ScalarCastFn) (sum, checked []exec.VectorKernel) {
	return cumulativeSumKernels(false, cast), cumulativeSumKernels(true, cast)
}
