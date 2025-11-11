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

package stringutils

import (
	"slices"
	"strings"
)

// ContainsStrings searches a slice of strings for a case-sensitive match
//
// Now equivalent to the standard library [slice.Contains].
func ContainsStrings(coll []string, item string) bool {
	return slices.Contains(coll, item)
}

// ContainsStringsCI searches a slice of strings for a case-insensitive match
func ContainsStringsCI(coll []string, item string) bool {
	return slices.ContainsFunc(coll, func(e string) bool {
		return strings.EqualFold(e, item)
	})
}
