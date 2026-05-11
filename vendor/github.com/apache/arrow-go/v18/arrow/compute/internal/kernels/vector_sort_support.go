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

//go:build go1.22

// Support for vector_sort.go: ordering primitives for Apache Arrow compute sort semantics
// (CompareTypeValues-style helpers).
//
// Sort compares logical rows in random order (stable_sort / merge). For multi-chunk columns,
// a dense logical-row→(chunk, offset) table gives O(1) per compare (faster here than
// ChunkResolver under random access). Single-chunk columns skip the table entirely.

package kernels

import (
	"bytes"
	"cmp"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/float16"
)

// logicalRowMap is a precomputed map from logical row index to chunk index and in-chunk offset.
// One struct per row (not two parallel int slices) so chunk/local live in the same cache line and
// pair(i,j) touches two contiguous cells instead of four scattered int loads.
type rowMapCell struct {
	chunk int
	local int
}

type logicalRowMap struct {
	cells []rowMapCell
}

func newLogicalRowMap(chunks []arrow.Array, numRows int) logicalRowMap {
	cells := make([]rowMapCell, numRows)
	g := 0
	for ci, arr := range chunks {
		for li := 0; li < arr.Len(); li++ {
			cells[g] = rowMapCell{chunk: ci, local: li}
			g++
		}
	}
	return logicalRowMap{cells: cells}
}

func (m *logicalRowMap) at(global uint64) (chunk, local int) {
	c := m.cells[global]
	return c.chunk, c.local
}

// pair resolves two logical rows in one call (hot path for compare); avoids double bounds/setup vs two at()s.
func (m *logicalRowMap) pair(i, j uint64) (ci, li, cj, lj int) {
	ai := m.cells[i]
	aj := m.cells[j]
	return ai.chunk, ai.local, aj.chunk, aj.local
}

func totalChunkRows(chunks []arrow.Array) (sum int) {
	for _, c := range chunks {
		sum += c.Len()
	}
	return sum
}

func chunksHaveNulls(chunks []arrow.Array) bool {
	for _, ch := range chunks {
		if ch.NullN() != 0 {
			return true
		}
	}
	return false
}

func compareOrdered[T cmp.Ordered](order SortOrder, vi, vj T) int {
	c := cmp.Compare(vi, vj)
	if order == Descending {
		return -c
	}
	return c
}

func compareBytesOrdered(order SortOrder, vi, vj []byte) int {
	c := bytes.Compare(vi, vj)
	if order == Descending {
		return -c
	}
	return c
}

func compareBoolOrdered(order SortOrder, vi, vj bool) int {
	var c int
	switch {
	case !vi && vj:
		c = -1
	case vi && !vj:
		c = 1
	default:
		c = 0
	}
	if order == Descending {
		return -c
	}
	return c
}

func compareFloatNaNs(order SortOrder, viNaN, vjNaN bool) (int, bool) {
	if viNaN && vjNaN {
		return 0, true
	}
	if viNaN {
		if order == Ascending {
			return 1, true
		}
		return -1, true
	}
	if vjNaN {
		if order == Ascending {
			return -1, true
		}
		return 1, true
	}
	return 0, false
}

func sortFloat64[T float16.Num | float32 | float64](v T) float64 {
	switch x := any(v).(type) {
	case float16.Num:
		return float64(x.Float32())
	case float32:
		return float64(x)
	case float64:
		return x
	default:
		panic("kernels: unreachable sortFloat64 type")
	}
}

func compareKeyedNulls(nullI, nullJ bool, key SortKey) (cmpVal int, stop bool) {
	if nullI && nullJ {
		return 0, true
	}
	if nullI {
		if key.NullPlacement == NullsAtStart {
			return -1, true
		}
		return 1, true
	}
	if nullJ {
		if key.NullPlacement == NullsAtStart {
			return 1, true
		}
		return -1, true
	}
	return 0, false
}

func compareCmperOrdered[T interface{ Cmp(T) int }](order SortOrder, vi, vj T) int {
	c := vi.Cmp(vj)
	if order == Descending {
		return -c
	}
	return c
}
