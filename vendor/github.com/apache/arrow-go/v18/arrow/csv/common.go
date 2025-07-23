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

// Package csv reads CSV files and presents the extracted data as records, also
// writes data as record into CSV files
package csv

import (
	"errors"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

var (
	ErrMismatchFields = errors.New("arrow/csv: number of records mismatch")
)

// Option configures a CSV reader/writer.
type Option func(config)
type config interface{}

// WithComma specifies the fields separation character used while parsing CSV files.
func WithComma(c rune) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.r.Comma = c
		case *Writer:
			cfg.w.Comma = c
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithComment specifies the comment character used while parsing CSV files.
func WithComment(c rune) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.r.Comment = c
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithAllocator specifies the Arrow memory allocator used while building records.
func WithAllocator(mem memory.Allocator) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.mem = mem
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithChunk specifies the chunk size used while parsing CSV files.
//
// If n is zero or 1, no chunking will take place and the reader will create
// one record per row.
// If n is greater than 1, chunks of n rows will be read.
// If n is negative, the reader will load the whole CSV file into memory and
// create one big record with all the rows.
func WithChunk(n int) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.chunk = n
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithCRLF specifies the line terminator used while writing CSV files.
// If useCRLF is true, \r\n is used as the line terminator, otherwise \n is used.
// The default value is false.
func WithCRLF(useCRLF bool) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Writer:
			cfg.w.UseCRLF = useCRLF
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithHeader enables or disables CSV-header handling.
func WithHeader(useHeader bool) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.header = useHeader
		case *Writer:
			cfg.header = useHeader
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithLazyQuotes sets csv parsing option to LazyQuotes
func WithLazyQuotes(useLazyQuotes bool) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.r.LazyQuotes = useLazyQuotes
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// DefaultNullValues is the set of values considered as NULL values by default
// when Reader is configured to handle NULL values.
var DefaultNullValues = []string{"", "NULL", "null"}

// WithNullReader sets options for a CSV Reader pertaining to NULL value
// handling. If stringsCanBeNull is true, then a string that matches one of the
// nullValues set will be interpreted as NULL. Numeric columns will be checked
// for nulls in all cases. If no nullValues arguments are passed in, the
// defaults set in NewReader() will be kept.
//
// When no NULL values is given, the default set is taken from DefaultNullValues.
func WithNullReader(stringsCanBeNull bool, nullValues ...string) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			cfg.stringsCanBeNull = stringsCanBeNull

			if len(nullValues) == 0 {
				nullValues = DefaultNullValues
			}
			cfg.nulls = make([]string, len(nullValues))
			copy(cfg.nulls, nullValues)
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithNullWriter sets the null string written for NULL values. The default is
// set in NewWriter().
func WithNullWriter(null string) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Writer:
			cfg.nullValue = null
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithBoolWriter override the default bool formatter with a function that returns
// a string representation of bool states. i.e. True, False, 1, 0
func WithBoolWriter(fmtr func(bool) string) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Writer:
			if fmtr != nil {
				cfg.boolFormatter = fmtr
			}
		default:
			panic(fmt.Errorf("arrow/csv: WithBoolWriter unknown config type %T", cfg))
		}
	}
}

// WithColumnTypes allows specifying optional per-column types (disabling
// type inference on those columns).
//
// Will panic if used in conjunction with an explicit schema.
func WithColumnTypes(types map[string]arrow.DataType) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			if cfg.schema != nil {
				panic(fmt.Errorf("%w: cannot use WithColumnTypes with explicit schema", arrow.ErrInvalid))
			}
			cfg.columnTypes = types
		default:
			panic(fmt.Errorf("%w: WithColumnTypes only allowed for csv reader", arrow.ErrInvalid))
		}
	}
}

// WithIncludeColumns indicates the names of the columns from the CSV file
// that should actually be read and converted (in the slice's order).
// If set and non-empty, columns not in this slice will be ignored.
//
// Will panic if used in conjunction with an explicit schema.
func WithIncludeColumns(cols []string) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Reader:
			if cfg.schema != nil {
				panic(fmt.Errorf("%w: cannot use WithIncludeColumns with explicit schema", arrow.ErrInvalid))
			}
			cfg.columnFilter = cols
		default:
			panic(fmt.Errorf("%w: WithIncludeColumns only allowed on csv Reader", arrow.ErrInvalid))
		}
	}
}

// WithStringsReplacer receives a replacer to be applied in the string fields
// of the CSV. This is useful to remove unwanted characters from the string.
func WithStringsReplacer(replacer *strings.Replacer) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Writer:
			cfg.stringReplacer = replacer.Replace
		default:
			panic(fmt.Errorf("arrow/csv: unknown config type %T", cfg))
		}
	}
}

// WithCustomTypeConverter allows specifying a custom type converter for the CSV writer.
//
// returns a slice of strings that must match the number of columns in the output csv.
// the second return value is a boolean that indicates if the conversion was handled.
// if it is set to false, the library will attempt to use default conversion.
//
// There are multiple ways to convert arrow types to strings, and depending on the goal, you may want to use a different one.
// One clear example is encoding binary types. The default behaviour is to encode them as base64 strings.
// If you want to customize this behaviour, you can use this option and use any other encoding, such as hex.
//
//	csv.WithCustomTypeConverter(func(typ arrow.DataType, col arrow.Array) (result []string, handled bool) {
//		// use hex encoding for binary types
//		if typ.ID() == arrow.BINARY {
//			result = make([]string, col.Len())
//			arr := col.(*array.Binary)
//			for i := 0; i < arr.Len(); i++ {
//				if !arr.IsValid(i) {
//					result[i] = "NULL"
//					continue
//				}
//				result[i] = fmt.Sprintf("\\x%x", arr.Value(i))
//			}
//			return result, true
//		}
//		// keep the default behavior for other types
//		return nil, false
//	})
func WithCustomTypeConverter(converter func(typ arrow.DataType, col arrow.Array) (result []string, handled bool)) Option {
	return func(cfg config) {
		switch cfg := cfg.(type) {
		case *Writer:
			cfg.customTypeConverter = converter
		default:
			panic(fmt.Errorf("%w: WithCustomTypeConverter only allowed on csv Writer", arrow.ErrInvalid))
		}
	}
}

func validateRead(schema *arrow.Schema) {
	for i, f := range schema.Fields() {
		if !readTypeSupported(f.Type) {
			panic(fmt.Errorf("arrow/csv: field %d (%s) has invalid data type %T", i, f.Name, f.Type))
		}
	}
}

func readTypeSupported(dt arrow.DataType) bool {
	switch dt := dt.(type) {
	case *arrow.BooleanType:
	case *arrow.Int8Type, *arrow.Int16Type, *arrow.Int32Type, *arrow.Int64Type:
	case *arrow.Uint8Type, *arrow.Uint16Type, *arrow.Uint32Type, *arrow.Uint64Type:
	case *arrow.Float16Type, *arrow.Float32Type, *arrow.Float64Type:
	case *arrow.StringType, *arrow.LargeStringType:
	case *arrow.TimestampType:
	case *arrow.Date32Type, *arrow.Date64Type:
	case *arrow.Decimal128Type, *arrow.Decimal256Type:
	case *arrow.MapType:
		return false
	case arrow.ListLikeType:
		return readTypeSupported(dt.Elem())
	case *arrow.BinaryType, *arrow.LargeBinaryType, *arrow.FixedSizeBinaryType:
	case arrow.ExtensionType:
	case *arrow.NullType:
	default:
		return false
	}
	return true
}
