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
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func (w *Writer) transformColToStringArr(typ arrow.DataType, col arrow.Array, stringsReplacer func(string) string) []string {
	res := make([]string, col.Len())
	switch typ.(type) {
	case *arrow.BooleanType:
		arr := col.(*array.Boolean)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = w.boolFormatter(arr.Value(i))
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Int8Type:
		arr := col.(*array.Int8)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatInt(int64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Int16Type:
		arr := col.(*array.Int16)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatInt(int64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Int32Type:
		arr := col.(*array.Int32)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatInt(int64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Int64Type:
		arr := col.(*array.Int64)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatInt(int64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Uint8Type:
		arr := col.(*array.Uint8)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatUint(uint64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Uint16Type:
		arr := col.(*array.Uint16)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatUint(uint64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Uint32Type:
		arr := col.(*array.Uint32)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatUint(uint64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Uint64Type:
		arr := col.(*array.Uint64)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatUint(uint64(arr.Value(i)), 10)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Float16Type:
		arr := col.(*array.Float16)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = arr.Value(i).String()
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Float32Type:
		arr := col.(*array.Float32)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatFloat(float64(arr.Value(i)), 'g', -1, 32)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Float64Type:
		arr := col.(*array.Float64)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = strconv.FormatFloat(float64(arr.Value(i)), 'g', -1, 64)
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.StringType:
		arr := col.(*array.String)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = stringsReplacer(arr.Value(i))
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.LargeStringType:
		arr := col.(*array.LargeString)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = stringsReplacer(arr.Value(i))
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Date32Type:
		arr := col.(*array.Date32)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = arr.Value(i).FormattedString()
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Date64Type:
		arr := col.(*array.Date64)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = arr.Value(i).FormattedString()
			} else {
				res[i] = w.nullValue
			}
		}

	case *arrow.TimestampType:
		arr := col.(*array.Timestamp)
		t := typ.(*arrow.TimestampType)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = arr.Value(i).ToTime(t.Unit).Format("2006-01-02 15:04:05.999999999")
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Decimal128Type:
		fieldType := typ.(*arrow.Decimal128Type)
		scale := fieldType.Scale
		precision := fieldType.Precision
		arr := col.(*array.Decimal128)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				f := (&big.Float{}).SetInt(arr.Value(i).BigInt())
				f.Quo(f, big.NewFloat(math.Pow10(int(scale))))
				res[i] = f.Text('g', int(precision))
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.Decimal256Type:
		fieldType := typ.(*arrow.Decimal256Type)
		scale := fieldType.Scale
		precision := fieldType.Precision
		arr := col.(*array.Decimal256)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				f := (&big.Float{}).SetInt(arr.Value(i).BigInt())
				f.Quo(f, big.NewFloat(math.Pow10(int(scale))))
				res[i] = f.Text('g', int(precision))
			} else {
				res[i] = w.nullValue
			}
		}
	case arrow.ListLikeType:
		arr := col.(array.ListLike)
		listVals := arr.ListValues()
		for i := 0; i < arr.Len(); i++ {
			if arr.IsNull(i) {
				res[i] = w.nullValue
				continue
			}
			start, end := arr.ValueOffsets(i)
			list := array.NewSlice(listVals, start, end)
			var b bytes.Buffer
			b.Write([]byte{'{'})
			writer := csv.NewWriter(&b)
			writer.Write(w.transformColToStringArr(list.DataType(), list, stringsReplacer))
			writer.Flush()
			b.Truncate(b.Len() - 1)
			b.Write([]byte{'}'})
			res[i] = b.String()
			list.Release()
		}
	case *arrow.BinaryType:
		arr := col.(*array.Binary)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = base64.StdEncoding.EncodeToString(arr.Value(i))
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.LargeBinaryType:
		arr := col.(*array.LargeBinary)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = base64.StdEncoding.EncodeToString(arr.Value(i))
			} else {
				res[i] = w.nullValue
			}
		}
	case *arrow.FixedSizeBinaryType:
		arr := col.(*array.FixedSizeBinary)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsValid(i) {
				res[i] = base64.StdEncoding.EncodeToString(arr.Value(i))
			} else {
				res[i] = w.nullValue
			}
		}
	case arrow.ExtensionType:
		arr := col.(array.ExtensionArray)
		for i := 0; i < arr.Len(); i++ {
			if arr.IsNull(i) {
				res[i] = w.nullValue
			} else {
				res[i] = arr.ValueStr(i)
			}
		}
	case *arrow.NullType:
		for i := 0; i < col.Len(); i++ {
			res[i] = w.nullValue
		}
	default:
		panic(fmt.Errorf("arrow/csv: field has unsupported data type %s", typ.String()))
	}
	return res
}
