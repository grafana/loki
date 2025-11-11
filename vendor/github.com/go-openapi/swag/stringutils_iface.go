// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package swag

import "github.com/go-openapi/swag/stringutils"

// ContainsStrings searches a slice of strings for a case-sensitive match.
//
// Deprecated: use [slices.Contains] or [stringutils.ContainsStrings] instead.
func ContainsStrings(coll []string, item string) bool {
	return stringutils.ContainsStrings(coll, item)
}

// ContainsStringsCI searches a slice of strings for a case-insensitive match.
//
// Deprecated: use [stringutils.ContainsStringsCI] instead.
func ContainsStringsCI(coll []string, item string) bool {
	return stringutils.ContainsStringsCI(coll, item)
}

// JoinByFormat joins a string array by a known format (e.g. swagger's collectionFormat attribute).
//
// Deprecated: use [stringutils.JoinByFormat] instead.
func JoinByFormat(data []string, format string) []string {
	return stringutils.JoinByFormat(data, format)
}

// SplitByFormat splits a string by a known format.
//
// Deprecated: use [stringutils.SplitByFormat] instead.
func SplitByFormat(data, format string) []string {
	return stringutils.SplitByFormat(data, format)
}
