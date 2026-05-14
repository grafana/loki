// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package helpers

import "regexp"

var (
	// Ignore generic poly errors that just say "none matched" since we get specific errors
	// But keep errors that say which subschemas matched (for multiple match scenarios)
	IgnorePattern     = `^'?(anyOf|allOf|oneOf|validation)'? failed(, none matched)?$`
	IgnorePolyPattern = `^'?(anyOf|allOf|oneOf)'? failed(, none matched)?$`
)

// IgnoreRegex is a regular expression that matches the IgnorePattern
var IgnoreRegex = regexp.MustCompile(IgnorePattern)

// IgnorePolyRegex is a regular expression that matches the IgnorePattern
var IgnorePolyRegex = regexp.MustCompile(IgnorePolyPattern)
