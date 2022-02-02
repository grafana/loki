//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package to

import "time"

// BoolPtr returns a pointer to the provided bool.
func BoolPtr(b bool) *bool {
	return &b
}

// Float32Ptr returns a pointer to the provided float32.
func Float32Ptr(i float32) *float32 {
	return &i
}

// Float64Ptr returns a pointer to the provided float64.
func Float64Ptr(i float64) *float64 {
	return &i
}

// Int32Ptr returns a pointer to the provided int32.
func Int32Ptr(i int32) *int32 {
	return &i
}

// Int64Ptr returns a pointer to the provided int64.
func Int64Ptr(i int64) *int64 {
	return &i
}

// StringPtr returns a pointer to the provided string.
func StringPtr(s string) *string {
	return &s
}

// TimePtr returns a pointer to the provided time.Time.
func TimePtr(t time.Time) *time.Time {
	return &t
}

// Int32PtrArray returns an array of *int32 from the specified values.
func Int32PtrArray(vals ...int32) []*int32 {
	arr := make([]*int32, len(vals))
	for i := range vals {
		arr[i] = Int32Ptr(vals[i])
	}
	return arr
}

// Int64PtrArray returns an array of *int64 from the specified values.
func Int64PtrArray(vals ...int64) []*int64 {
	arr := make([]*int64, len(vals))
	for i := range vals {
		arr[i] = Int64Ptr(vals[i])
	}
	return arr
}

// Float32PtrArray returns an array of *float32 from the specified values.
func Float32PtrArray(vals ...float32) []*float32 {
	arr := make([]*float32, len(vals))
	for i := range vals {
		arr[i] = Float32Ptr(vals[i])
	}
	return arr
}

// Float64PtrArray returns an array of *float64 from the specified values.
func Float64PtrArray(vals ...float64) []*float64 {
	arr := make([]*float64, len(vals))
	for i := range vals {
		arr[i] = Float64Ptr(vals[i])
	}
	return arr
}

// BoolPtrArray returns an array of *bool from the specified values.
func BoolPtrArray(vals ...bool) []*bool {
	arr := make([]*bool, len(vals))
	for i := range vals {
		arr[i] = BoolPtr(vals[i])
	}
	return arr
}

// StringPtrArray returns an array of *string from the specified values.
func StringPtrArray(vals ...string) []*string {
	arr := make([]*string, len(vals))
	for i := range vals {
		arr[i] = StringPtr(vals[i])
	}
	return arr
}

// TimePtrArray returns an array of *time.Time from the specified values.
func TimePtrArray(vals ...time.Time) []*time.Time {
	arr := make([]*time.Time, len(vals))
	for i := range vals {
		arr[i] = TimePtr(vals[i])
	}
	return arr
}
