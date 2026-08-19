// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"errors"
	"fmt"
	"math"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

type attributable interface {
	AttributeIndices() pcommon.Int32Slice
}

// FromAttributeIndices builds a [pcommon.Map] containing the attributes of a
// record.
// The record can be any struct that implements an `AttributeIndices` method.
// Updates made to the return map will not be applied back to the record.
func FromAttributeIndices(table KeyValueAndUnitSlice, record attributable, dic ProfilesDictionary) (pcommon.Map, error) {
	m := pcommon.NewMap()
	m.EnsureCapacity(record.AttributeIndices().Len())

	for i := 0; i < record.AttributeIndices().Len(); i++ {
		idx := record.AttributeIndices().At(i)
		if idx < 0 || int(idx) >= table.Len() {
			return m, fmt.Errorf("attribute index %d out of bounds [0, %d)", idx, table.Len())
		}
		kv := table.At(int(idx))
		keyIdx := kv.KeyStrindex()
		if keyIdx < 0 || int(keyIdx) >= dic.StringTable().Len() {
			return m, fmt.Errorf("key string index %d out of bounds [0, %d)", keyIdx, dic.StringTable().Len())
		}
		key := dic.StringTable().At(int(keyIdx))
		kv.Value().CopyTo(m.PutEmpty(key))
	}

	return m, nil
}

var errTooManyAttributeTableEntries = errors.New("too many entries in AttributeTable")

// SetAttribute updates an AttributeTable, adding or providing a value and
// returns its index.
func SetAttribute(table KeyValueAndUnitSlice, attr KeyValueAndUnit) (int32, error) {
	for j, a := range table.All() {
		if a.Equal(attr) {
			if j > math.MaxInt32 {
				return 0, errTooManyAttributeTableEntries
			}
			return int32(j), nil
		}
	}

	if table.Len() >= math.MaxInt32 {
		return 0, errTooManyAttributeTableEntries
	}

	attr.CopyTo(table.AppendEmpty())
	return int32(table.Len() - 1), nil
}
