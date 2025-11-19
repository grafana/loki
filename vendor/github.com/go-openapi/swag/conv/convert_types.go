// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package conv

// The original version of this file, eons ago, was taken from the aws go sdk

// Pointer returns a pointer to the value passed in.
func Pointer[T any](v T) *T {
	return &v
}

// Value returns a shallow copy of the value of the pointer passed in.
//
// If the pointer is nil, the returned value is the zero value.
func Value[T any](v *T) T {
	if v != nil {
		return *v
	}

	var zero T
	return zero
}

// PointerSlice converts a slice of values into a slice of pointers.
func PointerSlice[T any](src []T) []*T {
	dst := make([]*T, len(src))
	for i := 0; i < len(src); i++ {
		dst[i] = &(src[i])
	}
	return dst
}

// ValueSlice converts a slice of pointers into a slice of values.
//
// nil elements are zero values.
func ValueSlice[T any](src []*T) []T {
	dst := make([]T, len(src))
	for i := 0; i < len(src); i++ {
		if src[i] != nil {
			dst[i] = *(src[i])
		}
	}
	return dst
}

// PointerMap converts a map of values into a map of pointers.
func PointerMap[K comparable, T any](src map[K]T) map[K]*T {
	dst := make(map[K]*T)
	for k, val := range src {
		v := val
		dst[k] = &v
	}
	return dst
}

// ValueMap converts a map of pointers into a map of values.
//
// nil elements are skipped.
func ValueMap[K comparable, T any](src map[K]*T) map[K]T {
	dst := make(map[K]T)
	for k, val := range src {
		if val != nil {
			dst[k] = *val
		}
	}
	return dst
}
