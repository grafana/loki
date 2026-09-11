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

//go:build go1.18 && !noasm && !appengine

package kernels

import (
	"math"
	"unsafe"

	"golang.org/x/sys/cpu"
)

// The generated assembly reads the length through a 32-bit register. Larger
// Go slices must stay on the scalar path.
const maxNeonLength = int(math.MaxInt32)

func neonLengthFitsAssembly(length int) bool {
	return length <= maxNeonLength
}

//go:noescape
func _multiply_constant_int32_int32_neon(src, dest unsafe.Pointer, len int, factor int64)

func multiplyConstantInt32Int32Neon(in []int32, out []int32, factor int64) {
	if len(out) == 0 {
		return
	}
	if !neonLengthFitsAssembly(len(out)) {
		multiplyConstantGo(in, out, factor)
		return
	}
	_multiply_constant_int32_int32_neon(unsafe.Pointer(&in[0]), unsafe.Pointer(&out[0]), len(out), factor)
}

//go:noescape
func _multiply_constant_int32_int64_neon(src, dest unsafe.Pointer, len int, factor int64)

func multiplyConstantInt32Int64Neon(in []int32, out []int64, factor int64) {
	if len(out) == 0 {
		return
	}
	if !neonLengthFitsAssembly(len(out)) {
		multiplyConstantGo(in, out, factor)
		return
	}
	_multiply_constant_int32_int64_neon(unsafe.Pointer(&in[0]), unsafe.Pointer(&out[0]), len(out), factor)
}

//go:noescape
func _multiply_constant_int64_int32_neon(src, dest unsafe.Pointer, len int, factor int64)

func multiplyConstantInt64Int32Neon(in []int64, out []int32, factor int64) {
	if len(out) == 0 {
		return
	}
	if !neonLengthFitsAssembly(len(out)) {
		multiplyConstantGo(in, out, factor)
		return
	}
	_multiply_constant_int64_int32_neon(unsafe.Pointer(&in[0]), unsafe.Pointer(&out[0]), len(out), factor)
}

//go:noescape
func _multiply_constant_int64_int64_neon(src, dest unsafe.Pointer, len int, factor int64)

func multiplyConstantInt64Int64Neon(in []int64, out []int64, factor int64) {
	if len(out) == 0 {
		return
	}
	if !neonLengthFitsAssembly(len(out)) {
		multiplyConstantGo(in, out, factor)
		return
	}
	_multiply_constant_int64_int64_neon(unsafe.Pointer(&in[0]), unsafe.Pointer(&out[0]), len(out), factor)
}

func init() {
	if cpu.ARM64.HasASIMD {
		multiplyConstantInt32Int32 = multiplyConstantInt32Int32Neon
		multiplyConstantInt32Int64 = multiplyConstantInt32Int64Neon
		multiplyConstantInt64Int32 = multiplyConstantInt64Int32Neon
		multiplyConstantInt64Int64 = multiplyConstantInt64Int64Neon
	}
}
