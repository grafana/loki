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

package utils

// Min is a convenience Min function for int64
func Min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// MinInt is a convenience Min function for int
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Max is a convenience Max function for int64
func Max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// MaxInt is a convenience Max function for int
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
