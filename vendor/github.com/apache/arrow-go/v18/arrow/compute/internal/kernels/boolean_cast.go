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
	"strconv"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
)

func isNonZero[T arrow.FixedWidthType](ctx *exec.KernelCtx, in []T, out []byte) error {
	var zero T
	for i, v := range in {
		bitutil.SetBitTo(out, i, v != zero)
	}
	return nil
}

func numericToBoolKernel[T arrow.NumericType](typ arrow.Type) func(*exec.KernelCtx, []T, []byte) error {
	return func(ctx *exec.KernelCtx, in []T, out []byte) error {
		return numericToBoolNeon(typ, ctx, in, out)
	}
}

// GetBooleanCastKernels returns the slice of scalar kernels for casting
// values *to* a boolean type.
func GetBooleanCastKernels() []exec.ScalarKernel {
	kernels := GetCommonCastKernels(arrow.BOOL, exec.NewOutputType(arrow.FixedWidthTypes.Boolean))
	kernels = append(kernels, GetZeroCastKernel(arrow.BOOL,
		exec.NewExactInput(arrow.FixedWidthTypes.Boolean), exec.NewOutputType(arrow.FixedWidthTypes.Boolean)))

	out := exec.NewOutputType(arrow.FixedWidthTypes.Boolean)
	for _, ty := range numericTypes {
		var ex exec.ArrayKernelExec
		switch ty.ID() {
		case arrow.INT8:
			ex = ScalarUnaryBoolOutput(isNonZero[int8])
		case arrow.UINT8:
			ex = ScalarUnaryBoolOutput(isNonZero[uint8])
		case arrow.INT16:
			ex = ScalarUnaryBoolOutput(isNonZero[int16])
		case arrow.UINT16:
			ex = ScalarUnaryBoolOutput(isNonZero[uint16])
		case arrow.INT32:
			ex = ScalarUnaryBoolOutput(numericToBoolKernel[int32](arrow.INT32))
		case arrow.UINT32:
			ex = ScalarUnaryBoolOutput(numericToBoolKernel[uint32](arrow.UINT32))
		case arrow.INT64:
			ex = ScalarUnaryBoolOutput(numericToBoolKernel[int64](arrow.INT64))
		case arrow.UINT64:
			ex = ScalarUnaryBoolOutput(numericToBoolKernel[uint64](arrow.UINT64))
		case arrow.FLOAT32:
			ex = ScalarUnaryBoolOutput(numericToBoolKernel[float32](arrow.FLOAT32))
		case arrow.FLOAT64:
			ex = ScalarUnaryBoolOutput(numericToBoolKernel[float64](arrow.FLOAT64))
		}
		k := exec.NewScalarKernel(
			[]exec.InputType{exec.NewExactInput(ty)}, out, ex, nil)
		k.NullHandling = exec.NullIntersection
		k.MemAlloc = exec.MemPrealloc
		kernels = append(kernels, k)
	}

	for _, ty := range baseBinaryTypes {
		var ex exec.ArrayKernelExec
		switch ty.ID() {
		case arrow.BINARY, arrow.STRING:
			ex = ScalarUnaryNotNullBinaryArgBoolOut[int32](false, func(_ *exec.KernelCtx, b []byte, err *error) bool {
				v := *(*string)(unsafe.Pointer(&b))
				o, e := strconv.ParseBool(v)
				if e != nil {
					*err = e
				}
				return o
			})
		case arrow.LARGE_BINARY, arrow.LARGE_STRING:
			ex = ScalarUnaryNotNullBinaryArgBoolOut[int64](false, func(_ *exec.KernelCtx, b []byte, err *error) bool {
				v := *(*string)(unsafe.Pointer(&b))
				o, e := strconv.ParseBool(v)
				if e != nil {
					*err = e
				}
				return o
			})
		}
		k := exec.NewScalarKernel(
			[]exec.InputType{exec.NewExactInput(ty)}, out, ex, nil)
		k.NullHandling = exec.NullIntersection
		k.MemAlloc = exec.MemPrealloc
		kernels = append(kernels, k)
	}

	return kernels
}
