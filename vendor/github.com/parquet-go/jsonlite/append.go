package jsonlite

import (
	"encoding/base64"
	"iter"
	"strconv"
	"strings"
	"time"
)

// AppendFunc is a function that appends a value of type T to a byte slice.
type AppendFunc[T any] func([]byte, T) []byte

// IndentFunc returns the indentation string for a given nesting level.
type IndentFunc func(int) string

// Indent returns a string of spaces for the given nesting level (2 spaces per level).
// It is optimized to avoid heap allocations for common indentation depths.
func Indent(n int) string {
	const spaces = "                                "
	if 2*n <= len(spaces) {
		return spaces[:2*n]
	}
	return strings.Repeat(" ", 2*n)
}

// AppendArray appends a JSON array to b by iterating over seq and using fn
// to serialize each element.
func AppendArray[T any](b []byte, seq iter.Seq[T], fn AppendFunc[T]) []byte {
	b = append(b, '[')
	i := 0
	for elem := range seq {
		if i > 0 {
			b = append(b, ',')
		}
		b = fn(b, elem)
		i++
	}
	return append(b, ']')
}

// AppendIndentArray appends a pretty-printed JSON array to b by iterating over seq
// and using fn to serialize each element. The level parameter specifies the current
// nesting depth, and indent provides the indentation string for each level.
func AppendIndentArray[T any](b []byte, seq iter.Seq[T], fn AppendFunc[T], level int, indent IndentFunc) []byte {
	b = append(b, '[')
	i := 0
	for elem := range seq {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '\n')
		b = append(b, indent(level+1)...)
		b = fn(b, elem)
		i++
	}
	if i > 0 {
		b = append(b, '\n')
		b = append(b, indent(level)...)
	}
	return append(b, ']')
}

// AppendObject appends a JSON object to b by iterating over seq and using fn
// to serialize each value. Keys are automatically quoted.
func AppendObject[T any](b []byte, seq iter.Seq2[string, T], fn AppendFunc[T]) []byte {
	b = append(b, '{')
	i := 0
	for key, value := range seq {
		if i > 0 {
			b = append(b, ',')
		}
		b = AppendQuote(b, key)
		b = append(b, ':')
		b = fn(b, value)
		i++
	}
	return append(b, '}')
}

// AppendIndentObject appends a pretty-printed JSON object to b by iterating over seq
// and using fn to serialize each value. Keys are automatically quoted. The level
// parameter specifies the current nesting depth, and indent provides the indentation
// string for each level.
func AppendIndentObject[T any](b []byte, seq iter.Seq2[string, T], fn AppendFunc[T], level int, indent IndentFunc) []byte {
	b = append(b, '{')
	i := 0
	for key, value := range seq {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '\n')
		b = append(b, indent(level+1)...)
		b = AppendQuote(b, key)
		b = append(b, ':', ' ')
		b = fn(b, value)
		i++
	}
	if i > 0 {
		b = append(b, '\n')
		b = append(b, indent(level)...)
	}
	return append(b, '}')
}

// AppendNull appends a JSON null to b.
func AppendNull(b []byte) []byte { return append(b, "null"...) }

// AppendInt appends an integer as JSON to b.
func AppendInt(b []byte, n int64) []byte { return strconv.AppendInt(b, n, 10) }

// AppendUint appends an unsigned integer as JSON to b.
func AppendUint(b []byte, n uint64) []byte { return strconv.AppendUint(b, n, 10) }

// AppendFloat appends a floating point number as JSON to b.
func AppendFloat(b []byte, f float64) []byte { return strconv.AppendFloat(b, f, 'g', -1, 64) }

// AppendBool appends a boolean as JSON to b.
func AppendBool(b []byte, v bool) []byte { return strconv.AppendBool(b, v) }

// AppendTime appends a time.Time as a JSON quoted RFC3339Nano string to b.
func AppendTime(b []byte, t time.Time) []byte {
	b = append(b, '"')
	b = t.AppendFormat(b, time.RFC3339Nano)
	return append(b, '"')
}

// AppendBytes appends a byte slice as a base64-encoded JSON string to b.
func AppendBytes(b []byte, data []byte) []byte {
	b = append(b, '"')
	b = base64.StdEncoding.AppendEncode(b, data)
	return append(b, '"')
}
