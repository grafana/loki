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

//go:build go1.18 && arm64 && !noasm && !appengine

package kernels

import (
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"golang.org/x/exp/constraints"
	"golang.org/x/sys/cpu"
)

//go:noescape
func _arithmetic_binary_neon(typ int, op int8, inLeft, inRight, out unsafe.Pointer, len int)

func arithmeticNeon(typ arrow.Type, op ArithmeticOp, left, right, out []byte, len int) {
	if len == 0 {
		return
	}
	_arithmetic_binary_neon(int(typ), int8(op), unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), len)
}

//go:noescape
func _arithmetic_arr_scalar_neon(typ int, op int8, inLeft, inRight, out unsafe.Pointer, len int)

func arithmeticArrScalarNeon(typ arrow.Type, op ArithmeticOp, left []byte, right unsafe.Pointer, out []byte, len int) {
	if len == 0 {
		return
	}
	_arithmetic_arr_scalar_neon(int(typ), int8(op), unsafe.Pointer(&left[0]), right, unsafe.Pointer(&out[0]), len)
}

//go:noescape
func _arithmetic_scalar_arr_neon(typ int, op int8, inLeft, inRight, out unsafe.Pointer, len int)

func arithmeticScalarArrNeon(typ arrow.Type, op ArithmeticOp, left unsafe.Pointer, right, out []byte, len int) {
	if len == 0 {
		return
	}
	_arithmetic_scalar_arr_neon(int(typ), int8(op), left, unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), len)
}

//go:noescape
func _arithmetic_unary_same_types_neon(typ int, op int8, input, output unsafe.Pointer, len int)

func arithmeticUnaryNeon(typ arrow.Type, op ArithmeticOp, input, out []byte, len int) {
	if len == 0 {
		return
	}
	_arithmetic_unary_same_types_neon(int(typ), int8(op), unsafe.Pointer(&input[0]), unsafe.Pointer(&out[0]), len)
}

func normalizeNeonArithmeticOp(op ArithmeticOp) ArithmeticOp {
	switch op {
	case OpAddChecked:
		return OpAdd
	case OpSubChecked:
		return OpSub
	case OpMulChecked:
		return OpMul
	case OpAbsoluteValueChecked:
		return OpAbsoluteValue
	case OpNegateChecked:
		return OpNegate
	default:
		return op
	}
}

func neonIntegralBinarySupported(typ arrow.Type, op ArithmeticOp) bool {
	switch typ {
	case arrow.INT32, arrow.UINT32:
		return op == OpAdd || op == OpSub || op == OpMul
	case arrow.INT64, arrow.UINT64:
		return op == OpAdd || op == OpSub
	default:
		return false
	}
}

func neonIntegralUnarySupported(typ arrow.Type) bool {
	switch typ {
	case arrow.INT32, arrow.UINT32, arrow.INT64, arrow.UINT64:
		return true
	default:
		return false
	}
}

func getNeonArithmeticBinaryNumeric[T arrow.NumericType](op ArithmeticOp) binaryOps[T, T, T] {
	typ := arrow.GetType[T]()
	return binaryOps[T, T, T]{
		arrArr: func(_ *exec.KernelCtx, Arg0, Arg1, Out []T) error {
			arithmeticNeon(typ, op, arrow.GetBytes(Arg0), arrow.GetBytes(Arg1), arrow.GetBytes(Out), len(Arg0))
			return nil
		},
		arrScalar: func(_ *exec.KernelCtx, Arg0 []T, Arg1 T, Out []T) error {
			arithmeticArrScalarNeon(typ, op, arrow.GetBytes(Arg0), unsafe.Pointer(&Arg1), arrow.GetBytes(Out), len(Arg0))
			return nil
		},
		scalarArr: func(_ *exec.KernelCtx, Arg0 T, Arg1, Out []T) error {
			arithmeticScalarArrNeon(typ, op, unsafe.Pointer(&Arg0), arrow.GetBytes(Arg1), arrow.GetBytes(Out), len(Arg1))
			return nil
		},
	}
}

func getArithmeticOpIntegral[InT, OutT arrow.UintType | arrow.IntType](op ArithmeticOp) exec.ArrayKernelExec {
	typ := arrow.GetType[InT]()
	if cpu.ARM64.HasASIMD && typ == arrow.GetType[OutT]() {
		switch op {
		case OpAdd, OpSub, OpMul:
			if neonIntegralBinarySupported(typ, op) {
				return ScalarBinary(getNeonArithmeticBinaryNumeric[InT](op))
			}
		case OpAbsoluteValue, OpNegate:
			if neonIntegralUnarySupported(typ) {
				return ScalarUnary(func(_ *exec.KernelCtx, arg, out []InT) error {
					arithmeticUnaryNeon(typ, op, arrow.GetBytes(arg), arrow.GetBytes(out), len(arg))
					return nil
				})
			}
		}
	}

	// no SIMD for POWER or SQRT functions
	// integral checked funcs need to use NotNull versions
	return getGoArithmeticOpIntegral[InT, OutT](op)
}

func getArithmeticOpFloating[InT, OutT constraints.Float](op ArithmeticOp) exec.ArrayKernelExec {
	if cpu.ARM64.HasASIMD && arrow.GetType[InT]() == arrow.GetType[OutT]() {
		typ := arrow.GetType[InT]()
		switch op {
		case OpAdd, OpSub, OpAddChecked, OpSubChecked, OpMul, OpMulChecked:
			return ScalarBinary(getNeonArithmeticBinaryNumeric[InT](normalizeNeonArithmeticOp(op)))
		case OpAbsoluteValue, OpAbsoluteValueChecked, OpNegate, OpNegateChecked:
			return ScalarUnary(func(_ *exec.KernelCtx, arg, out []InT) error {
				arithmeticUnaryNeon(typ, normalizeNeonArithmeticOp(op), arrow.GetBytes(arg), arrow.GetBytes(out), len(arg))
				return nil
			})
		}
	}

	// no SIMD for POWER or SQRT functions
	return getGoArithmeticOpFloating[InT, OutT](op)
}
