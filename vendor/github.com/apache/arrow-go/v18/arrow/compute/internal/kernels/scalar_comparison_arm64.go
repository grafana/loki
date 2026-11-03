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
	"golang.org/x/sys/cpu"
)

const (
	neonCompareArrayArray = iota
	neonCompareArrayScalar
	neonCompareScalarArray
)

//go:noescape
func _comparison_neon(typ int, op int, shape int, left, right, out unsafe.Pointer, groups int64)

//go:noescape
func _comparison_narrow_neon(typ int, op int, shape int, left, right, out unsafe.Pointer, groups int64)

func neonComparisonSupported(typ arrow.Type) bool {
	switch typ {
	case arrow.INT8, arrow.UINT8, arrow.INT16, arrow.UINT16,
		arrow.INT32, arrow.UINT32, arrow.INT64, arrow.UINT64,
		arrow.FLOAT32, arrow.FLOAT64:
		return true
	default:
		return false
	}
}

func compareNeon(typ arrow.Type, op CompareOperator, width, shape int, left, right, out []byte, offset int, fallback binaryKernel) {
	n := len(left) / width
	if shape == neonCompareScalarArray {
		n = len(right) / width
	}
	if n == 0 {
		return
	}

	if offset != 0 {
		prefix := min(n, 8-offset)
		switch shape {
		case neonCompareArrayArray:
			fallback(left[:prefix*width], right[:prefix*width], out, offset)
		case neonCompareArrayScalar:
			fallback(left[:prefix*width], right, out, offset)
		case neonCompareScalarArray:
			fallback(left, right[:prefix*width], out, offset)
		}
		if prefix == n {
			return
		}

		switch shape {
		case neonCompareArrayArray:
			left = left[prefix*width:]
			right = right[prefix*width:]
		case neonCompareArrayScalar:
			left = left[prefix*width:]
		case neonCompareScalarArray:
			right = right[prefix*width:]
		}
		out = out[1:]
		n -= prefix
	}

	bulk := n &^ 7
	if bulk != 0 {
		assemblyLeft, assemblyRight := left, right
		switch shape {
		case neonCompareArrayArray:
			assemblyLeft = left[:bulk*width]
			assemblyRight = right[:bulk*width]
		case neonCompareArrayScalar:
			assemblyLeft = left[:bulk*width]
		case neonCompareScalarArray:
			assemblyRight = right[:bulk*width]
		}
		if width <= 2 {
			_comparison_narrow_neon(int(typ), int(op), shape,
				unsafe.Pointer(&assemblyLeft[0]), unsafe.Pointer(&assemblyRight[0]), unsafe.Pointer(&out[0]), int64(bulk/8))
		} else {
			_comparison_neon(int(typ), int(op), shape,
				unsafe.Pointer(&assemblyLeft[0]), unsafe.Pointer(&assemblyRight[0]), unsafe.Pointer(&out[0]), int64(bulk/8))
		}
	}

	if tail := n - bulk; tail != 0 {
		out = out[bulk/8:]
		switch shape {
		case neonCompareArrayArray:
			fallback(left[bulk*width:bulk*width+tail*width], right[bulk*width:bulk*width+tail*width], out, 0)
		case neonCompareArrayScalar:
			fallback(left[bulk*width:bulk*width+tail*width], right, out, 0)
		case neonCompareScalarArray:
			fallback(left, right[bulk*width:bulk*width+tail*width], out, 0)
		}
	}
}

func genCompareKernel[T arrow.NumericType](op CompareOperator) *CompareData {
	ty := arrow.GetType[T]()
	fallback := genGoCompareKernel(getCmpOp[T](op))
	if !cpu.ARM64.HasASIMD || !neonComparisonSupported(ty) {
		return fallback
	}

	width := int(unsafe.Sizeof(T(0)))
	return &CompareData{
		funcAA: func(left, right, out []byte, offset int) {
			compareNeon(ty, op, width, neonCompareArrayArray, left, right, out, offset, fallback.funcAA)
		},
		funcAS: func(left, right, out []byte, offset int) {
			compareNeon(ty, op, width, neonCompareArrayScalar, left, right, out, offset, fallback.funcAS)
		},
		funcSA: func(left, right, out []byte, offset int) {
			compareNeon(ty, op, width, neonCompareScalarArray, left, right, out, offset, fallback.funcSA)
		},
	}
}
