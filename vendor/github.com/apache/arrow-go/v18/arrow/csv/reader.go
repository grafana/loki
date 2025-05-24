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

package csv

import (
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/arrow/float16"
	"github.com/apache/arrow-go/v18/arrow/internal/debug"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Reader wraps encoding/csv.Reader and creates array.Records from a schema.
type Reader struct {
	r      *csv.Reader
	schema *arrow.Schema

	refs atomic.Int64
	bld  *array.RecordBuilder
	cur  arrow.Record
	err  error

	chunk int
	done  bool
	next  func() bool

	mem memory.Allocator

	header bool
	once   sync.Once

	fieldConverter []func(val string)
	columnFilter   []string
	columnTypes    map[string]arrow.DataType
	conversions    []conversionColumn

	stringsCanBeNull bool
	nulls            []string
}

// NewInferringReader creates a CSV reader that attempts to infer the types
// and column names from the data in the first row of the CSV file.
//
// This can be further customized using the WithColumnTypes and
// WithIncludeColumns options.
// For BinaryType the reader will use base64 decoding with padding as per base64.StdDecoding.
func NewInferringReader(r io.Reader, opts ...Option) *Reader {
	rr := &Reader{
		r:                csv.NewReader(r),
		chunk:            1,
		stringsCanBeNull: false,
	}
	rr.refs.Add(1)
	rr.r.ReuseRecord = true
	for _, opt := range opts {
		opt(rr)
	}

	if rr.mem == nil {
		rr.mem = memory.DefaultAllocator
	}

	switch {
	case rr.chunk < 0:
		rr.next = rr.nextall
	case rr.chunk > 1:
		rr.next = rr.nextn
	default:
		rr.next = rr.next1
	}

	return rr
}

// NewReader returns a reader that reads from the CSV file and creates
// arrow.Records from the given schema.
//
// NewReader panics if the given schema contains fields that have types that are not
// primitive types.
func NewReader(r io.Reader, schema *arrow.Schema, opts ...Option) *Reader {
	validate(schema)

	rr := &Reader{
		r:                csv.NewReader(r),
		schema:           schema,
		chunk:            1,
		stringsCanBeNull: false,
	}
	rr.refs.Add(1)
	rr.r.ReuseRecord = true
	for _, opt := range opts {
		opt(rr)
	}

	if rr.mem == nil {
		rr.mem = memory.DefaultAllocator
	}

	rr.bld = array.NewRecordBuilder(rr.mem, rr.schema)

	switch {
	case rr.chunk < 0:
		rr.next = rr.nextall
	case rr.chunk > 1:
		rr.next = rr.nextn
	default:
		rr.next = rr.next1
	}

	return rr
}

func (r *Reader) readHeader() error {
	// if we have an explicit schema and we want to skip the header
	// then just return and do everything normally
	if r.schema != nil && !r.header {
		return nil
	}

	// either we need this first line for the header line
	// or we are going to need this line to infer types
	records, err := r.r.Read()
	if err != nil {
		return fmt.Errorf("arrow/csv: could not read header from file: %w", err)
	}

	// if we have an explicit schema, then r.header must be true otherwise
	// we would have skipped this via the first line of this func
	if r.schema != nil {
		if len(records) != len(r.schema.Fields()) {
			return ErrMismatchFields
		}

		fields := make([]arrow.Field, len(records))
		for idx, name := range records {
			fields[idx] = r.schema.Field(idx)
			fields[idx].Name = name
		}

		meta := r.schema.Metadata()
		r.schema = arrow.NewSchema(fields, &meta)
		r.bld = array.NewRecordBuilder(r.mem, r.schema)
		return nil
	}

	// we're going to need to infer some column types
	r.conversions = make([]conversionColumn, 0, len(records))
	if len(r.columnFilter) == 0 {
		for i, rec := range records {
			// if we are skipping the header, autogenerate field names
			// using "f<n>" e.g. f0, f1, ....
			if !r.header {
				rec = fmt.Sprintf("f%d", i)
			}
			var dt arrow.DataType
			if len(r.columnTypes) > 0 {
				dt = r.columnTypes[rec]
			}
			r.conversions = append(r.conversions, conversionColumn{name: rec, index: i, typ: dt})
		}
	} else {
		// include columns from columnFilter (in that order)
		// compute the indices of columns in the csv file
		colIndices := make(map[string]int)
		for i, n := range records {
			// if we are skipping the header, autogenerate field names
			// using "f<n>" e.g. f0, f1, ....
			if !r.header {
				n = fmt.Sprintf("f%d", i)
			}
			colIndices[n] = i
		}

		for _, n := range r.columnFilter {
			idx, ok := colIndices[n]
			if !ok {
				return fmt.Errorf("%w: column '%s' in included columns, but doesn't exist in CSV file",
					ErrMismatchFields, n)
			}
			var dt arrow.DataType
			if len(r.columnTypes) > 0 {
				dt = r.columnTypes[n]
			}
			r.conversions = append(r.conversions, conversionColumn{name: n, index: idx, typ: dt})
		}
		r.columnFilter = nil
	}
	r.columnTypes = nil
	return nil
}

// Err returns the last error encountered during the iteration over the
// underlying CSV file.
func (r *Reader) Err() error { return r.err }

func (r *Reader) Schema() *arrow.Schema { return r.schema }

// Record returns the current record that has been extracted from the
// underlying CSV file.
// It is valid until the next call to Next.
func (r *Reader) Record() arrow.Record { return r.cur }

// Next returns whether a Record could be extracted from the underlying CSV file.
//
// Next panics if the number of records extracted from a CSV row does not match
// the number of fields of the associated schema. If a parse failure occurs, Next
// will return true and the Record will contain nulls where failures occurred.
// Subsequent calls to Next will return false - The user should check Err() after
// each call to Next to check if an error took place.
func (r *Reader) Next() bool {
	r.once.Do(func() {
		r.err = r.readHeader()
		if r.err == nil && r.schema != nil {
			// Create a table of functions that will parse columns. This optimization
			// allows us to specialize the implementation of each column's decoding
			// and hoist type-based branches outside the inner loop.
			r.fieldConverter = make([]func(string), len(r.schema.Fields()))
			for idx := range r.schema.Fields() {
				r.fieldConverter[idx] = r.initFieldConverter(r.bld.Field(idx))
			}
		}
	})

	if r.cur != nil {
		r.cur.Release()
		r.cur = nil
	}

	if r.err != nil || r.done {
		return false
	}

	return r.next()
}

// next1 reads one row from the CSV file and creates a single Record
// from that row.
func (r *Reader) next1() bool {
	var recs []string
	recs, r.err = r.r.Read()
	if r.err != nil {
		r.done = true
		if errors.Is(r.err, io.EOF) {
			r.err = nil
		}
		return false
	}

	r.validate(recs)
	r.read(recs)
	r.cur = r.bld.NewRecord()

	return true
}

// nextall reads the whole CSV file into memory and creates one single
// Record from all the CSV rows.
func (r *Reader) nextall() bool {
	defer func() {
		r.done = true
	}()

	var recs [][]string

	recs, r.err = r.r.ReadAll()
	if r.err != nil {
		return false
	}

	for _, rec := range recs {
		r.validate(rec)
		r.read(rec)
	}
	r.cur = r.bld.NewRecord()

	return true
}

// nextn reads n rows from the CSV file, where n is the chunk size, and creates
// a Record from these rows.
func (r *Reader) nextn() bool {
	var (
		recs []string
		n    = 0
		err  error
	)

	for i := 0; i < r.chunk && !r.done; i++ {
		recs, err = r.r.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				r.err = err
			}
			r.done = true
			break
		}

		r.validate(recs)
		r.read(recs)
		n++
	}

	if r.err != nil {
		r.done = true
	}

	r.cur = r.bld.NewRecord()
	return n > 0
}

func (r *Reader) validate(recs []string) {
	if r.err != nil {
		return
	}

	if r.bld == nil {
		// initialize the record builder in the case where we're inferring a schema
		r.fieldConverter = make([]func(val string), len(recs))
		fieldList := make([]arrow.Field, len(r.conversions))
		for idx, cc := range r.conversions {
			fieldList[idx].Name = cc.name
			fieldList[idx].Nullable = true
			fieldList[idx].Type = cc.inferType(recs[cc.index])
		}

		r.schema = arrow.NewSchema(fieldList, nil)
		r.bld = array.NewRecordBuilder(r.mem, r.schema)
		for idx, cc := range r.conversions {
			r.fieldConverter[cc.index] = r.initFieldConverter(r.bld.Field(idx))
		}
		for idx, fc := range r.fieldConverter {
			if fc == nil {
				r.fieldConverter[idx] = func(string) {}
			}
		}
	}

	if len(recs) != len(r.fieldConverter) {
		r.err = ErrMismatchFields
		return
	}
}

func (r *Reader) isNull(val string) bool {
	for _, v := range r.nulls {
		if v == val {
			return true
		}
	}
	return false
}

func (r *Reader) read(recs []string) {
	for i, str := range recs {
		r.fieldConverter[i](str)
	}
}

func (r *Reader) initFieldConverter(bldr array.Builder) func(string) {
	switch dt := bldr.Type().(type) {
	case *arrow.BooleanType:
		return func(str string) {
			r.parseBool(bldr, str)
		}
	case *arrow.Int8Type:
		return func(str string) {
			r.parseInt8(bldr, str)
		}
	case *arrow.Int16Type:
		return func(str string) {
			r.parseInt16(bldr, str)
		}
	case *arrow.Int32Type:
		return func(str string) {
			r.parseInt32(bldr, str)
		}
	case *arrow.Int64Type:
		return func(str string) {
			r.parseInt64(bldr, str)
		}
	case *arrow.Uint8Type:
		return func(str string) {
			r.parseUint8(bldr, str)
		}
	case *arrow.Uint16Type:
		return func(str string) {
			r.parseUint16(bldr, str)
		}
	case *arrow.Uint32Type:
		return func(str string) {
			r.parseUint32(bldr, str)
		}
	case *arrow.Uint64Type:
		return func(str string) {
			r.parseUint64(bldr, str)
		}
	case *arrow.Float16Type:
		return func(str string) {
			r.parseFloat16(bldr, str)
		}
	case *arrow.Float32Type:
		return func(str string) {
			r.parseFloat32(bldr, str)
		}
	case *arrow.Float64Type:
		return func(str string) {
			r.parseFloat64(bldr, str)
		}
	case *arrow.StringType:
		// specialize the implementation when we know we cannot have nulls
		if r.stringsCanBeNull {
			return func(str string) {
				if r.isNull(str) {
					bldr.AppendNull()
				} else {
					bldr.(*array.StringBuilder).Append(str)
				}
			}
		} else {
			return func(str string) {
				bldr.(*array.StringBuilder).Append(str)
			}
		}
	case *arrow.LargeStringType:
		// specialize the implementation when we know we cannot have nulls
		if r.stringsCanBeNull {
			return func(str string) {
				if r.isNull(str) {
					bldr.AppendNull()
				} else {
					bldr.(*array.LargeStringBuilder).Append(str)
				}
			}
		} else {
			return func(str string) {
				bldr.(*array.LargeStringBuilder).Append(str)
			}
		}
	case *arrow.TimestampType:
		return func(str string) {
			r.parseTimestamp(bldr, str, dt.Unit)
		}
	case *arrow.Date32Type:
		return func(str string) {
			r.parseDate32(bldr, str)
		}
	case *arrow.Date64Type:
		return func(str string) {
			r.parseDate64(bldr, str)
		}
	case *arrow.Time32Type:
		return func(str string) {
			r.parseTime32(bldr, str, dt.Unit)
		}
	case *arrow.Decimal128Type:
		return func(str string) {
			r.parseDecimal128(bldr, str, dt.Precision, dt.Scale)
		}
	case *arrow.Decimal256Type:
		return func(str string) {
			r.parseDecimal256(bldr, str, dt.Precision, dt.Scale)
		}
	case *arrow.FixedSizeListType:
		return func(s string) {
			r.parseFixedSizeList(bldr.(*array.FixedSizeListBuilder), s, int(dt.Len()))
		}
	case arrow.ListLikeType:
		return func(s string) {
			r.parseListLike(bldr.(array.ListLikeBuilder), s)
		}
	case *arrow.BinaryType:
		return func(s string) {
			r.parseBinaryType(bldr, s)
		}
	case *arrow.LargeBinaryType:
		return func(s string) {
			r.parseLargeBinaryType(bldr, s)
		}
	case *arrow.FixedSizeBinaryType:
		return func(s string) {
			r.parseFixedSizeBinaryType(bldr, s, dt.Bytes())
		}
	case arrow.ExtensionType:
		return func(s string) {
			r.parseExtension(bldr, s)
		}
	default:
		panic(fmt.Errorf("arrow/csv: unhandled field type %T", bldr.Type()))
	}
}

func (r *Reader) parseBool(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseBool(str)
	if err != nil {
		r.err = fmt.Errorf("%w: unrecognized boolean: %s", err, str)
		field.AppendNull()
		return
	}

	field.(*array.BooleanBuilder).Append(v)
}

func (r *Reader) parseInt8(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseInt(str, 10, 8)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Int8Builder).Append(int8(v))
}

func (r *Reader) parseInt16(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseInt(str, 10, 16)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Int16Builder).Append(int16(v))
}

func (r *Reader) parseInt32(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseInt(str, 10, 32)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Int32Builder).Append(int32(v))
}

func (r *Reader) parseInt64(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseInt(str, 10, 64)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Int64Builder).Append(v)
}

func (r *Reader) parseUint8(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseUint(str, 10, 8)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Uint8Builder).Append(uint8(v))
}

func (r *Reader) parseUint16(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseUint(str, 10, 16)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Uint16Builder).Append(uint16(v))
}

func (r *Reader) parseUint32(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseUint(str, 10, 32)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Uint32Builder).Append(uint32(v))
}

func (r *Reader) parseUint64(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseUint(str, 10, 64)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.Uint64Builder).Append(v)
}

func (r *Reader) parseFloat16(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseFloat(str, 32)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Float16Builder).Append(float16.New(float32(v)))
}

func (r *Reader) parseFloat32(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseFloat(str, 32)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Float32Builder).Append(float32(v))
}

func (r *Reader) parseFloat64(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := strconv.ParseFloat(str, 64)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Float64Builder).Append(v)
}

// parses timestamps using millisecond precision
func (r *Reader) parseTimestamp(field array.Builder, str string, unit arrow.TimeUnit) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	v, err := arrow.TimestampFromString(str, unit)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}

	field.(*array.TimestampBuilder).Append(v)
}

func (r *Reader) parseDate32(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	tm, err := time.Parse("2006-01-02", str)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Date32Builder).Append(arrow.Date32FromTime(tm))
}

func (r *Reader) parseDate64(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	tm, err := time.Parse("2006-01-02", str)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Date64Builder).Append(arrow.Date64FromTime(tm))
}

func (r *Reader) parseTime32(field array.Builder, str string, unit arrow.TimeUnit) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	val, err := arrow.Time32FromString(str, unit)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Time32Builder).Append(val)
}

func (r *Reader) parseDecimal128(field array.Builder, str string, prec, scale int32) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	val, err := decimal128.FromString(str, prec, scale)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Decimal128Builder).Append(val)
}

func (r *Reader) parseDecimal256(field array.Builder, str string, prec, scale int32) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}

	val, err := decimal256.FromString(str, prec, scale)
	if err != nil && r.err == nil {
		r.err = err
		field.AppendNull()
		return
	}
	field.(*array.Decimal256Builder).Append(val)
}

func (r *Reader) parseListLike(field array.ListLikeBuilder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}
	if !(strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}")) {
		r.err = errors.New("invalid list format. should start with '{' and end with '}'")
		return
	}
	str = strings.Trim(str, "{}")
	field.Append(true)
	if len(str) == 0 {
		// we don't want to create the csv reader if we already know the
		// string is empty
		return
	}
	valueBldr := field.ValueBuilder()
	reader := csv.NewReader(strings.NewReader(str))
	items, err := reader.Read()
	if err != nil {
		r.err = err
		return
	}
	for _, str := range items {
		r.initFieldConverter(valueBldr)(str)
	}
}

func (r *Reader) parseFixedSizeList(field *array.FixedSizeListBuilder, str string, n int) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}
	if !(strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}")) {
		r.err = errors.New("invalid list format. should start with '{' and end with '}'")
		return
	}
	str = strings.Trim(str, "{}")
	field.Append(true)
	if len(str) == 0 {
		// we don't want to create the csv reader if we already know the
		// string is empty
		return
	}
	valueBldr := field.ValueBuilder()
	reader := csv.NewReader(strings.NewReader(str))
	items, err := reader.Read()
	if err != nil {
		r.err = err
		return
	}
	if len(items) == n {
		for _, str := range items {
			r.initFieldConverter(valueBldr)(str)
		}
	} else {
		r.err = fmt.Errorf("%w: fixed size list items should match the fixed size list length, expected %d, got %d", arrow.ErrInvalid, n, len(items))
	}
}

func (r *Reader) parseBinaryType(field array.Builder, str string) {
	// specialize the implementation when we know we cannot have nulls
	if r.isNull(str) {
		field.AppendNull()
		return
	}
	decodedVal, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		r.err = fmt.Errorf("cannot decode base64 string %s", str)
		field.AppendNull()
		return
	}

	field.(*array.BinaryBuilder).Append(decodedVal)
}

func (r *Reader) parseLargeBinaryType(field array.Builder, str string) {
	// specialize the implementation when we know we cannot have nulls
	if r.isNull(str) {
		field.AppendNull()
		return
	}
	decodedVal, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		r.err = fmt.Errorf("cannot decode base64 string %s", str)
		field.AppendNull()
		return
	}

	field.(*array.BinaryBuilder).Append(decodedVal)
}

func (r *Reader) parseFixedSizeBinaryType(field array.Builder, str string, byteWidth int) {
	// specialize the implementation when we know we cannot have nulls
	if r.isNull(str) {
		field.AppendNull()
		return
	}
	decodedVal, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		r.err = fmt.Errorf("cannot decode base64 string %s", str)
		field.AppendNull()
		return
	}

	if len(decodedVal) == byteWidth {
		field.(*array.FixedSizeBinaryBuilder).Append(decodedVal)
	} else {
		r.err = fmt.Errorf("%w: the length of fixed size binary value should match the fixed size binary byte width, expected %d, got %d", arrow.ErrInvalid, byteWidth, len(decodedVal))
	}
}

func (r *Reader) parseExtension(field array.Builder, str string) {
	if r.isNull(str) {
		field.AppendNull()
		return
	}
	if err := field.AppendValueFromString(str); err != nil {
		r.err = err
		return
	}
}

// Retain increases the reference count by 1.
// Retain may be called simultaneously from multiple goroutines.
func (r *Reader) Retain() {
	r.refs.Add(1)
}

// Release decreases the reference count by 1.
// When the reference count goes to zero, the memory is freed.
// Release may be called simultaneously from multiple goroutines.
func (r *Reader) Release() {
	debug.Assert(r.refs.Load() > 0, "too many releases")

	if r.refs.Add(-1) == 0 {
		if r.cur != nil {
			r.cur.Release()
		}
	}
}

type conversionColumn struct {
	name  string
	index int
	typ   arrow.DataType
}

func (c conversionColumn) inferType(v string) arrow.DataType {
	if c.typ != nil {
		return c.typ
	}

	var err error
	c.typ = arrow.PrimitiveTypes.Int64
	for {
		// attempt to parse
		if err = tryParse(v, c.typ); err == nil {
			return c.typ
		}

		switch dt := c.typ.(type) {
		case *arrow.Int64Type:
			c.typ = arrow.FixedWidthTypes.Boolean
		case *arrow.BooleanType:
			c.typ = arrow.FixedWidthTypes.Date32
		case *arrow.Date32Type:
			c.typ = arrow.FixedWidthTypes.Time32s
		case *arrow.Time32Type:
			c.typ = &arrow.TimestampType{Unit: arrow.Second}
		case *arrow.TimestampType:
			if dt.TimeZone == "" {
				if dt.Unit == arrow.Second {
					c.typ = &arrow.TimestampType{Unit: arrow.Nanosecond}
				} else {
					c.typ = &arrow.TimestampType{Unit: arrow.Second, TimeZone: "UTC"}
				}
			} else {
				if dt.Unit == arrow.Second {
					c.typ = &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"}
				} else {
					c.typ = arrow.PrimitiveTypes.Float64
				}
			}
		case *arrow.Float64Type:
			c.typ = arrow.BinaryTypes.String
		case *arrow.StringType:
			// binary is the fallback type
			return arrow.BinaryTypes.Binary
		}
	}
}

func tryParse(val string, dt arrow.DataType) error {
	switch dt := dt.(type) {
	case *arrow.Int64Type:
		_, err := strconv.ParseInt(val, 10, 64)
		return err
	case *arrow.BooleanType:
		_, err := strconv.ParseBool(val)
		return err
	case *arrow.Date32Type:
		_, err := time.Parse("2006-01-02", val)
		return err
	case *arrow.Time32Type:
		_, err := arrow.Time32FromString(val, dt.Unit)
		return err
	case *arrow.TimestampType:
		_, err := arrow.TimestampFromString(val, dt.Unit)
		return err
	case *arrow.Float64Type:
		_, err := strconv.ParseFloat(val, 64)
		return err
	case *arrow.StringType:
		if !utf8.ValidString(val) {
			return arrow.ErrInvalid
		}
		return nil
	case *arrow.BinaryType:
		_, err := base64.RawStdEncoding.DecodeString(val)
		return err
	}
	panic("shouldn't end up here")
}

var _ array.RecordReader = (*Reader)(nil)
