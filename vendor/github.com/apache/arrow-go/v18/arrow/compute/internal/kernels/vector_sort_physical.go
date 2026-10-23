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
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

//go:build go1.22

package kernels

import (
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// physicalColumnBase holds chunked data and row resolution shared by monomorphic sort columns.
// Each Arrow physical type has its own comparator struct embedding this (no compare func pointer).
type physicalColumnBase struct {
	chunks        []arrow.Array
	rowMap        logicalRowMap
	validityNulls bool
}

func newPhysicalColumnBase(chunks []arrow.Array, numRows int, validityNulls bool) physicalColumnBase {
	var rowMap logicalRowMap
	if len(chunks) > 1 {
		rowMap = newLogicalRowMap(chunks, numRows)
	}
	return physicalColumnBase{chunks: chunks, rowMap: rowMap, validityNulls: validityNulls}
}

// Pointer receivers: a value receiver would copy chunks + logicalRowMap slice headers on every
// compare (pair/isNull/cell), which is measurable on large n log n sorts.
func (b *physicalColumnBase) pair(i, j uint64) (arrI, arrJ arrow.Array, li, lj int) {
	if len(b.chunks) == 1 {
		arrI = b.chunks[0]
		arrJ = arrI
		li = int(i)
		lj = int(j)
		return
	}
	ci, li2, cj, lj2 := b.rowMap.pair(i, j)
	arrI = b.chunks[ci]
	arrJ = b.chunks[cj]
	li, lj = li2, lj2
	return
}

func (b *physicalColumnBase) isNullAtGlobal(row uint64) bool {
	if len(b.chunks) == 1 {
		return b.chunks[0].IsNull(int(row))
	}
	ci, li := b.rowMap.at(row)
	return b.chunks[ci].IsNull(li)
}

func (b *physicalColumnBase) cell(row uint64) (ch arrow.Array, li int) {
	if len(b.chunks) == 1 {
		return b.chunks[0], int(row)
	}
	ci, li := b.rowMap.at(row)
	return b.chunks[ci], li
}

func (b *physicalColumnBase) columnHasValidityNulls() bool { return b.validityNulls }

// --- Monomorphic comparators (one concrete *array type each; mirrors C++ ConcreteColumnComparator<T>) ---

type physicalSortInt8Column struct{ base physicalColumnBase }

func newPhysicalSortInt8Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortInt8Column {
	return &physicalSortInt8Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortInt8Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Int8)
	b := aj.(*array.Int8)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortInt8Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortInt8Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortInt8Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortInt8Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortInt16Column struct{ base physicalColumnBase }

func newPhysicalSortInt16Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortInt16Column {
	return &physicalSortInt16Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortInt16Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Int16)
	b := aj.(*array.Int16)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortInt16Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortInt16Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortInt16Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortInt16Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortInt32Column struct{ base physicalColumnBase }

func newPhysicalSortInt32Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortInt32Column {
	return &physicalSortInt32Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortInt32Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Int32)
	b := aj.(*array.Int32)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortInt32Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortInt32Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortInt32Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortInt32Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDate32Column struct{ base physicalColumnBase }

func newPhysicalSortDate32Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortDate32Column {
	return &physicalSortDate32Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDate32Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Date32)
	b := aj.(*array.Date32)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDate32Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDate32Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortDate32Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDate32Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortTime32Column struct{ base physicalColumnBase }

func newPhysicalSortTime32Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortTime32Column {
	return &physicalSortTime32Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortTime32Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Time32)
	b := aj.(*array.Time32)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortTime32Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortTime32Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortTime32Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortTime32Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortInt64Column struct{ base physicalColumnBase }

func newPhysicalSortInt64Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortInt64Column {
	return &physicalSortInt64Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortInt64Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Int64)
	b := aj.(*array.Int64)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortInt64Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortInt64Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortInt64Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortInt64Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDate64Column struct{ base physicalColumnBase }

func newPhysicalSortDate64Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortDate64Column {
	return &physicalSortDate64Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDate64Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Date64)
	b := aj.(*array.Date64)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDate64Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDate64Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortDate64Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDate64Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortTime64Column struct{ base physicalColumnBase }

func newPhysicalSortTime64Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortTime64Column {
	return &physicalSortTime64Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortTime64Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Time64)
	b := aj.(*array.Time64)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortTime64Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortTime64Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortTime64Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortTime64Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortTimestampColumn struct{ base physicalColumnBase }

func newPhysicalSortTimestampColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortTimestampColumn {
	return &physicalSortTimestampColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortTimestampColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Timestamp)
	b := aj.(*array.Timestamp)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortTimestampColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortTimestampColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortTimestampColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortTimestampColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDurationColumn struct{ base physicalColumnBase }

func newPhysicalSortDurationColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortDurationColumn {
	return &physicalSortDurationColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDurationColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Duration)
	b := aj.(*array.Duration)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDurationColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDurationColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortDurationColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDurationColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortUint8Column struct{ base physicalColumnBase }

func newPhysicalSortUint8Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortUint8Column {
	return &physicalSortUint8Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortUint8Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Uint8)
	b := aj.(*array.Uint8)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortUint8Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortUint8Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortUint8Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortUint8Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortUint16Column struct{ base physicalColumnBase }

func newPhysicalSortUint16Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortUint16Column {
	return &physicalSortUint16Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortUint16Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Uint16)
	b := aj.(*array.Uint16)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortUint16Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortUint16Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortUint16Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortUint16Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortUint32Column struct{ base physicalColumnBase }

func newPhysicalSortUint32Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortUint32Column {
	return &physicalSortUint32Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortUint32Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Uint32)
	b := aj.(*array.Uint32)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortUint32Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortUint32Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortUint32Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortUint32Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortUint64Column struct{ base physicalColumnBase }

func newPhysicalSortUint64Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortUint64Column {
	return &physicalSortUint64Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortUint64Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Uint64)
	b := aj.(*array.Uint64)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortUint64Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortUint64Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortUint64Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortUint64Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortFloat16Column struct{ base physicalColumnBase }

func newPhysicalSortFloat16Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortFloat16Column {
	return &physicalSortFloat16Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortFloat16Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Float16)
	b := aj.(*array.Float16)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	vi := sortFloat64(a.Value(li))
	vj := sortFloat64(b.Value(lj))
	viNaN := math.IsNaN(vi)
	vjNaN := math.IsNaN(vj)
	if cmpVal, ok := compareFloatNaNs(key.Order, viNaN, vjNaN); ok {
		return cmpVal
	}
	return compareOrdered(key.Order, vi, vj)
}

func (c *physicalSortFloat16Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortFloat16Column) hasNullLikeValues() bool  { return true }
func (c *physicalSortFloat16Column) isNullLikeAt(row uint64) bool {
	ch, li := c.base.cell(row)
	if ch.IsNull(li) {
		return false
	}
	return math.IsNaN(sortFloat64(ch.(*array.Float16).Value(li)))
}
func (c *physicalSortFloat16Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortFloat32Column struct{ base physicalColumnBase }

func newPhysicalSortFloat32Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortFloat32Column {
	return &physicalSortFloat32Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortFloat32Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Float32)
	b := aj.(*array.Float32)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	vi := sortFloat64(a.Value(li))
	vj := sortFloat64(b.Value(lj))
	viNaN := math.IsNaN(vi)
	vjNaN := math.IsNaN(vj)
	if cmpVal, ok := compareFloatNaNs(key.Order, viNaN, vjNaN); ok {
		return cmpVal
	}
	return compareOrdered(key.Order, vi, vj)
}

func (c *physicalSortFloat32Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortFloat32Column) hasNullLikeValues() bool  { return true }
func (c *physicalSortFloat32Column) isNullLikeAt(row uint64) bool {
	ch, li := c.base.cell(row)
	if ch.IsNull(li) {
		return false
	}
	return math.IsNaN(sortFloat64(ch.(*array.Float32).Value(li)))
}
func (c *physicalSortFloat32Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortFloat64Column struct{ base physicalColumnBase }

func newPhysicalSortFloat64Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortFloat64Column {
	return &physicalSortFloat64Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortFloat64Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Float64)
	b := aj.(*array.Float64)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	vi := sortFloat64(a.Value(li))
	vj := sortFloat64(b.Value(lj))
	viNaN := math.IsNaN(vi)
	vjNaN := math.IsNaN(vj)
	if cmpVal, ok := compareFloatNaNs(key.Order, viNaN, vjNaN); ok {
		return cmpVal
	}
	return compareOrdered(key.Order, vi, vj)
}

func (c *physicalSortFloat64Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortFloat64Column) hasNullLikeValues() bool  { return true }
func (c *physicalSortFloat64Column) isNullLikeAt(row uint64) bool {
	ch, li := c.base.cell(row)
	if ch.IsNull(li) {
		return false
	}
	return math.IsNaN(sortFloat64(ch.(*array.Float64).Value(li)))
}
func (c *physicalSortFloat64Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDecimal32Column struct{ base physicalColumnBase }

func newPhysicalSortDecimal32Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortDecimal32Column {
	return &physicalSortDecimal32Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDecimal32Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Decimal32)
	b := aj.(*array.Decimal32)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareCmperOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDecimal32Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDecimal32Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortDecimal32Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDecimal32Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDecimal64Column struct{ base physicalColumnBase }

func newPhysicalSortDecimal64Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortDecimal64Column {
	return &physicalSortDecimal64Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDecimal64Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Decimal64)
	b := aj.(*array.Decimal64)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareCmperOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDecimal64Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDecimal64Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortDecimal64Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDecimal64Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDecimal128Column struct{ base physicalColumnBase }

func newPhysicalSortDecimal128Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortDecimal128Column {
	return &physicalSortDecimal128Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDecimal128Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Decimal128)
	b := aj.(*array.Decimal128)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareCmperOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDecimal128Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDecimal128Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortDecimal128Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDecimal128Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDecimal256Column struct{ base physicalColumnBase }

func newPhysicalSortDecimal256Column(chunks []arrow.Array, numRows int, vn bool) *physicalSortDecimal256Column {
	return &physicalSortDecimal256Column{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDecimal256Column) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Decimal256)
	b := aj.(*array.Decimal256)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareCmperOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDecimal256Column) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDecimal256Column) hasNullLikeValues() bool  { return false }
func (c *physicalSortDecimal256Column) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDecimal256Column) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortMonthIntervalColumn struct{ base physicalColumnBase }

func newPhysicalSortMonthIntervalColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortMonthIntervalColumn {
	return &physicalSortMonthIntervalColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortMonthIntervalColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.MonthInterval)
	b := aj.(*array.MonthInterval)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortMonthIntervalColumn) isNullAt(row uint64) bool {
	return c.base.isNullAtGlobal(row)
}
func (c *physicalSortMonthIntervalColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortMonthIntervalColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortMonthIntervalColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortDayTimeColumn struct{ base physicalColumnBase }

func newPhysicalSortDayTimeColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortDayTimeColumn {
	return &physicalSortDayTimeColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortDayTimeColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.DayTimeInterval)
	b := aj.(*array.DayTimeInterval)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareCmperOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortDayTimeColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortDayTimeColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortDayTimeColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortDayTimeColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortMonthDayNanoColumn struct{ base physicalColumnBase }

func newPhysicalSortMonthDayNanoColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortMonthDayNanoColumn {
	return &physicalSortMonthDayNanoColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortMonthDayNanoColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.MonthDayNanoInterval)
	b := aj.(*array.MonthDayNanoInterval)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareCmperOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortMonthDayNanoColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortMonthDayNanoColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortMonthDayNanoColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortMonthDayNanoColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortBoolColumn struct{ base physicalColumnBase }

func newPhysicalSortBoolColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortBoolColumn {
	return &physicalSortBoolColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortBoolColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Boolean)
	b := aj.(*array.Boolean)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareBoolOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortBoolColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortBoolColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortBoolColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortBoolColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortStringColumn struct{ base physicalColumnBase }

func newPhysicalSortStringColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortStringColumn {
	return &physicalSortStringColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortStringColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.String)
	b := aj.(*array.String)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortStringColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortStringColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortStringColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortStringColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortLargeStringColumn struct{ base physicalColumnBase }

func newPhysicalSortLargeStringColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortLargeStringColumn {
	return &physicalSortLargeStringColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortLargeStringColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.LargeString)
	b := aj.(*array.LargeString)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortLargeStringColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortLargeStringColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortLargeStringColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortLargeStringColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortBinaryColumn struct{ base physicalColumnBase }

func newPhysicalSortBinaryColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortBinaryColumn {
	return &physicalSortBinaryColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortBinaryColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.Binary)
	b := aj.(*array.Binary)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareBytesOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortBinaryColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortBinaryColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortBinaryColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortBinaryColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortLargeBinaryColumn struct{ base physicalColumnBase }

func newPhysicalSortLargeBinaryColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortLargeBinaryColumn {
	return &physicalSortLargeBinaryColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortLargeBinaryColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.LargeBinary)
	b := aj.(*array.LargeBinary)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareBytesOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortLargeBinaryColumn) isNullAt(row uint64) bool { return c.base.isNullAtGlobal(row) }
func (c *physicalSortLargeBinaryColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortLargeBinaryColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortLargeBinaryColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}

type physicalSortFixedSizeBinaryColumn struct{ base physicalColumnBase }

func newPhysicalSortFixedSizeBinaryColumn(chunks []arrow.Array, numRows int, vn bool) *physicalSortFixedSizeBinaryColumn {
	return &physicalSortFixedSizeBinaryColumn{base: newPhysicalColumnBase(chunks, numRows, vn)}
}

func (c *physicalSortFixedSizeBinaryColumn) compareRowsForKey(i, j uint64, key SortKey) int {
	ai, aj, li, lj := c.base.pair(i, j)
	a := ai.(*array.FixedSizeBinary)
	b := aj.(*array.FixedSizeBinary)
	if c.base.validityNulls {
		if v, stop := compareKeyedNulls(a.IsNull(li), b.IsNull(lj), key); stop {
			return v
		}
	}
	return compareBytesOrdered(key.Order, a.Value(li), b.Value(lj))
}

func (c *physicalSortFixedSizeBinaryColumn) isNullAt(row uint64) bool {
	return c.base.isNullAtGlobal(row)
}
func (c *physicalSortFixedSizeBinaryColumn) hasNullLikeValues() bool  { return false }
func (c *physicalSortFixedSizeBinaryColumn) isNullLikeAt(uint64) bool { return false }
func (c *physicalSortFixedSizeBinaryColumn) columnHasValidityNulls() bool {
	return c.base.columnHasValidityNulls()
}
