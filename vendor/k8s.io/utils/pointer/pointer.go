/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pointer

import (
	"fmt"
	"reflect"
	"time"
)

// AllPtrFieldsNil tests whether all pointer fields in a struct are nil.  This is useful when,
// for example, an API struct is handled by plugins which need to distinguish
// "no plugin accepted this spec" from "this spec is empty".
//
// This function is only valid for structs and pointers to structs.  Any other
// type will cause a panic.  Passing a typed nil pointer will return true.
func AllPtrFieldsNil(obj interface{}) bool {
	v := reflect.ValueOf(obj)
	if !v.IsValid() {
		panic(fmt.Sprintf("reflect.ValueOf() produced a non-valid Value for %#v", obj))
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.Ptr && !v.Field(i).IsNil() {
			return false
		}
	}
	return true
}

// Int returns a pointer to an int
func Int(i int) *int {
	return &i
}

// IntPtr is a function variable referring to Int.
//
// Deprecated: Use Int instead.
var IntPtr = Int // for back-compat

// IntDeref dereferences the int ptr and returns it if not nil, or else
// returns def.
func IntDeref(ptr *int, def int) int {
	if ptr != nil {
		return *ptr
	}
	return def
}

// IntPtrDerefOr is a function variable referring to IntDeref.
//
// Deprecated: Use IntDeref instead.
var IntPtrDerefOr = IntDeref // for back-compat

// Int32 returns a pointer to an int32.
func Int32(i int32) *int32 {
	return &i
}

// Int32Ptr is a function variable referring to Int32.
//
// Deprecated: Use Int32 instead.
var Int32Ptr = Int32 // for back-compat

// Int32Deref dereferences the int32 ptr and returns it if not nil, or else
// returns def.
func Int32Deref(ptr *int32, def int32) int32 {
	if ptr != nil {
		return *ptr
	}
	return def
}

// Int32PtrDerefOr is a function variable referring to Int32Deref.
//
// Deprecated: Use Int32Deref instead.
var Int32PtrDerefOr = Int32Deref // for back-compat

// Int32Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
func Int32Equal(a, b *int32) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Uint returns a pointer to an uint
func Uint(i uint) *uint {
	return &i
}

// UintPtr is a function variable referring to Uint.
//
// Deprecated: Use Uint instead.
var UintPtr = Uint // for back-compat

// UintDeref dereferences the uint ptr and returns it if not nil, or else
// returns def.
func UintDeref(ptr *uint, def uint) uint {
	if ptr != nil {
		return *ptr
	}
	return def
}

// UintPtrDerefOr is a function variable referring to UintDeref.
//
// Deprecated: Use UintDeref instead.
var UintPtrDerefOr = UintDeref // for back-compat

// Uint32 returns a pointer to an uint32.
func Uint32(i uint32) *uint32 {
	return &i
}

// Uint32Ptr is a function variable referring to Uint32.
//
// Deprecated: Use Uint32 instead.
var Uint32Ptr = Uint32 // for back-compat

// Uint32Deref dereferences the uint32 ptr and returns it if not nil, or else
// returns def.
func Uint32Deref(ptr *uint32, def uint32) uint32 {
	if ptr != nil {
		return *ptr
	}
	return def
}

// Uint32PtrDerefOr is a function variable referring to Uint32Deref.
//
// Deprecated: Use Uint32Deref instead.
var Uint32PtrDerefOr = Uint32Deref // for back-compat

// Uint32Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
func Uint32Equal(a, b *uint32) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Int64 returns a pointer to an int64.
func Int64(i int64) *int64 {
	return &i
}

// Int64Ptr is a function variable referring to Int64.
//
// Deprecated: Use Int64 instead.
var Int64Ptr = Int64 // for back-compat

// Int64Deref dereferences the int64 ptr and returns it if not nil, or else
// returns def.
func Int64Deref(ptr *int64, def int64) int64 {
	if ptr != nil {
		return *ptr
	}
	return def
}

// Int64PtrDerefOr is a function variable referring to Int64Deref.
//
// Deprecated: Use Int64Deref instead.
var Int64PtrDerefOr = Int64Deref // for back-compat

// Int64Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
func Int64Equal(a, b *int64) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Uint64 returns a pointer to an uint64.
func Uint64(i uint64) *uint64 {
	return &i
}

// Uint64Ptr is a function variable referring to Uint64.
//
// Deprecated: Use Uint64 instead.
var Uint64Ptr = Uint64 // for back-compat

// Uint64Deref dereferences the uint64 ptr and returns it if not nil, or else
// returns def.
func Uint64Deref(ptr *uint64, def uint64) uint64 {
	if ptr != nil {
		return *ptr
	}
	return def
}

// Uint64PtrDerefOr is a function variable referring to Uint64Deref.
//
// Deprecated: Use Uint64Deref instead.
var Uint64PtrDerefOr = Uint64Deref // for back-compat

// Uint64Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
func Uint64Equal(a, b *uint64) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Bool returns a pointer to a bool.
func Bool(b bool) *bool {
	return &b
}

// BoolPtr is a function variable referring to Bool.
//
// Deprecated: Use Bool instead.
var BoolPtr = Bool // for back-compat

// BoolDeref dereferences the bool ptr and returns it if not nil, or else
// returns def.
func BoolDeref(ptr *bool, def bool) bool {
	if ptr != nil {
		return *ptr
	}
	return def
}

// BoolPtrDerefOr is a function variable referring to BoolDeref.
//
// Deprecated: Use BoolDeref instead.
var BoolPtrDerefOr = BoolDeref // for back-compat

// BoolEqual returns true if both arguments are nil or both arguments
// dereference to the same value.
func BoolEqual(a, b *bool) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// String returns a pointer to a string.
func String(s string) *string {
	return &s
}

// StringPtr is a function variable referring to String.
//
// Deprecated: Use String instead.
var StringPtr = String // for back-compat

// StringDeref dereferences the string ptr and returns it if not nil, or else
// returns def.
func StringDeref(ptr *string, def string) string {
	if ptr != nil {
		return *ptr
	}
	return def
}

// StringPtrDerefOr is a function variable referring to StringDeref.
//
// Deprecated: Use StringDeref instead.
var StringPtrDerefOr = StringDeref // for back-compat

// StringEqual returns true if both arguments are nil or both arguments
// dereference to the same value.
func StringEqual(a, b *string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Float32 returns a pointer to a float32.
func Float32(i float32) *float32 {
	return &i
}

// Float32Ptr is a function variable referring to Float32.
//
// Deprecated: Use Float32 instead.
var Float32Ptr = Float32

// Float32Deref dereferences the float32 ptr and returns it if not nil, or else
// returns def.
func Float32Deref(ptr *float32, def float32) float32 {
	if ptr != nil {
		return *ptr
	}
	return def
}

// Float32PtrDerefOr is a function variable referring to Float32Deref.
//
// Deprecated: Use Float32Deref instead.
var Float32PtrDerefOr = Float32Deref // for back-compat

// Float32Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
func Float32Equal(a, b *float32) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Float64 returns a pointer to a float64.
func Float64(i float64) *float64 {
	return &i
}

// Float64Ptr is a function variable referring to Float64.
//
// Deprecated: Use Float64 instead.
var Float64Ptr = Float64

// Float64Deref dereferences the float64 ptr and returns it if not nil, or else
// returns def.
func Float64Deref(ptr *float64, def float64) float64 {
	if ptr != nil {
		return *ptr
	}
	return def
}

// Float64PtrDerefOr is a function variable referring to Float64Deref.
//
// Deprecated: Use Float64Deref instead.
var Float64PtrDerefOr = Float64Deref // for back-compat

// Float64Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
func Float64Equal(a, b *float64) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// Duration returns a pointer to a time.Duration.
func Duration(d time.Duration) *time.Duration {
	return &d
}

// DurationDeref dereferences the time.Duration ptr and returns it if not nil, or else
// returns def.
func DurationDeref(ptr *time.Duration, def time.Duration) time.Duration {
	if ptr != nil {
		return *ptr
	}
	return def
}

// DurationEqual returns true if both arguments are nil or both arguments
// dereference to the same value.
func DurationEqual(a, b *time.Duration) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}
