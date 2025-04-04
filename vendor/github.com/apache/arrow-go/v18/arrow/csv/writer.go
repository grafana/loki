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
	"encoding/csv"
	"io"
	"strconv"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
)

// Writer wraps encoding/csv.Writer and writes arrow.Record based on a schema.
type Writer struct {
	boolFormatter  func(bool) string
	header         bool
	nullValue      string
	stringReplacer func(string) string
	once           sync.Once
	schema         *arrow.Schema
	w              *csv.Writer
}

// NewWriter returns a writer that writes arrow.Records to the CSV file
// with the given schema.
//
// NewWriter panics if the given schema contains fields that have types that are not
// primitive types.
// For BinaryType the writer will use base64 encoding with padding as per base64.StdEncoding.
func NewWriter(w io.Writer, schema *arrow.Schema, opts ...Option) *Writer {
	validate(schema)

	ww := &Writer{
		boolFormatter:  strconv.FormatBool,                 // override by passing WithBoolWriter() as an option
		nullValue:      "NULL",                             // override by passing WithNullWriter() as an option
		stringReplacer: func(x string) string { return x }, // override by passing WithStringsReplacer() as an option
		schema:         schema,
		w:              csv.NewWriter(w),
	}
	for _, opt := range opts {
		opt(ww)
	}

	return ww
}

func (w *Writer) Schema() *arrow.Schema { return w.schema }

// Write writes a single Record as one row to the CSV file
func (w *Writer) Write(record arrow.Record) error {
	if !record.Schema().Equal(w.schema) {
		return ErrMismatchFields
	}

	var err error
	if w.header {
		w.once.Do(func() {
			err = w.writeHeader()
		})
		if err != nil {
			return err
		}
	}

	recs := make([][]string, record.NumRows())
	for i := range recs {
		recs[i] = make([]string, record.NumCols())
	}

	for j, col := range record.Columns() {
		rows := w.transformColToStringArr(w.schema.Field(j).Type, col, w.stringReplacer)
		for i, row := range rows {
			recs[i][j] = row
		}
	}

	return w.w.WriteAll(recs)
}

// Flush writes any buffered data to the underlying csv Writer.
// If an error occurred during the Flush, return it
func (w *Writer) Flush() error {
	w.w.Flush()
	return w.w.Error()
}

// Error reports any error that has occurred during a previous Write or Flush.
func (w *Writer) Error() error {
	return w.w.Error()
}

func (w *Writer) writeHeader() error {
	headers := make([]string, len(w.schema.Fields()))
	for i := range headers {
		headers[i] = w.schema.Field(i).Name
	}
	if err := w.w.Write(headers); err != nil {
		return err
	}
	return nil
}
