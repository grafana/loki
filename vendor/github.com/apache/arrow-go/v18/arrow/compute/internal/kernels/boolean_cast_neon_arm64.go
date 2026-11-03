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
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"golang.org/x/sys/cpu"
)

func numericToBoolNeon[T arrow.NumericType](typ arrow.Type, ctx *exec.KernelCtx, in []T, out []byte) error {
	if !cpu.ARM64.HasASIMD {
		return isNonZero(ctx, in, out)
	}

	var zero T
	bulk := len(in) &^ 7
	if bulk != 0 {
		left := arrow.GetBytes(in[:bulk])
		right := unsafe.Slice((*byte)(unsafe.Pointer(&zero)), int(unsafe.Sizeof(zero)))
		_comparison_neon(int(typ), int(CmpNE), neonCompareArrayScalar,
			unsafe.Pointer(&left[0]), unsafe.Pointer(&right[0]), unsafe.Pointer(&out[0]), int64(bulk/8))
	}
	for i, v := range in[bulk:] {
		bitutil.SetBitTo(out, bulk+i, v != zero)
	}
	return nil
}
