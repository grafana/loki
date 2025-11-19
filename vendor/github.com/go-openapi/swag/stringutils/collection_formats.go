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

import "strings"

const (
	// collectionFormatComma = "csv"
	collectionFormatSpace = "ssv"
	collectionFormatTab   = "tsv"
	collectionFormatPipe  = "pipes"
	collectionFormatMulti = "multi"

	collectionFormatDefaultSep = ","
)

// JoinByFormat joins a string array by a known format (e.g. swagger's collectionFormat attribute):
//
//	ssv: space separated value
//	tsv: tab separated value
//	pipes: pipe (|) separated value
//	csv: comma separated value (default)
func JoinByFormat(data []string, format string) []string {
	if len(data) == 0 {
		return data
	}
	var sep string
	switch format {
	case collectionFormatSpace:
		sep = " "
	case collectionFormatTab:
		sep = "\t"
	case collectionFormatPipe:
		sep = "|"
	case collectionFormatMulti:
		return data
	default:
		sep = collectionFormatDefaultSep
	}
	return []string{strings.Join(data, sep)}
}

// SplitByFormat splits a string by a known format:
//
//	ssv: space separated value
//	tsv: tab separated value
//	pipes: pipe (|) separated value
//	csv: comma separated value (default)
func SplitByFormat(data, format string) []string {
	if data == "" {
		return nil
	}
	var sep string
	switch format {
	case collectionFormatSpace:
		sep = " "
	case collectionFormatTab:
		sep = "\t"
	case collectionFormatPipe:
		sep = "|"
	case collectionFormatMulti:
		return nil
	default:
		sep = collectionFormatDefaultSep
	}
	var result []string
	for _, s := range strings.Split(data, sep) {
		if ts := strings.TrimSpace(s); ts != "" {
			result = append(result, ts)
		}
	}
	return result
}
