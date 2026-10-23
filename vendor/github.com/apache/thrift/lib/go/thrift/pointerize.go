/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements. See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership. The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License. You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package thrift

// Pointer is the generic (type parameter) version of the helper function that
// converts types to pointer types.
//
//go:fix inline
func Pointer[T any](v T) *T {
	return new(v)
}

///////////////////////////////////////////////////////////////////////////////
// This file is home to helpers that convert from various base types to
// respective pointer types. This is necessary because Go does not permit
// references to constants, nor can a pointer type to base type be allocated
// and initialized in a single expression.
//
// E.g., this is not allowed:
//
//    var ip *int = &5
//
// But this *is* allowed:
//
//    func IntPtr(i int) *int { return &i }
//    var ip *int = IntPtr(5)
//
// Since pointers to base types are commonplace as [optional] fields in
// exported thrift structs, we factor such helpers here.
///////////////////////////////////////////////////////////////////////////////

//go:fix inline
func Float32Ptr(v float32) *float32 { return new(v) }

//go:fix inline
func Float64Ptr(v float64) *float64 { return new(v) }

//go:fix inline
func IntPtr(v int) *int { return new(v) }

//go:fix inline
func Int8Ptr(v int8) *int8 { return new(v) }

//go:fix inline
func Int16Ptr(v int16) *int16 { return new(v) }

//go:fix inline
func Int32Ptr(v int32) *int32 { return new(v) }

//go:fix inline
func Int64Ptr(v int64) *int64 { return new(v) }

//go:fix inline
func StringPtr(v string) *string { return new(v) }

//go:fix inline
func Uint32Ptr(v uint32) *uint32 { return new(v) }

//go:fix inline
func Uint64Ptr(v uint64) *uint64 { return new(v) }

//go:fix inline
func BoolPtr(v bool) *bool { return new(v) }

//go:fix inline
func ByteSlicePtr(v []byte) *[]byte { return new(v) }

//go:fix inline
func TuuidPtr(v Tuuid) *Tuuid { return new(v) }
