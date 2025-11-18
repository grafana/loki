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

import (
	"strconv"
)

// FormatInteger turns an integer type into a string.
func FormatInteger[T Signed](value T) string {
	return strconv.FormatInt(int64(value), 10)
}

// FormatUinteger turns an unsigned integer type into a string.
func FormatUinteger[T Unsigned](value T) string {
	return strconv.FormatUint(uint64(value), 10)
}

// FormatFloat turns a floating point numerical value into a string.
func FormatFloat[T Float](value T) string {
	return strconv.FormatFloat(float64(value), 'f', -1, bitsize(value))
}

// FormatBool turns a boolean into a string.
func FormatBool(value bool) string {
	return strconv.FormatBool(value)
}
