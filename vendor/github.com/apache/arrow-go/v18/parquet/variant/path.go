// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package variant

import (
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// pathElem is one step of a VariantPath: an object field when isField is set, else an array index.
type pathElem struct {
	name    string
	index   int
	isField bool
}

// VariantPath is an ordered list of steps to navigate into a variant value. The
// zero value is the root path; extend it with Field and Index.
type VariantPath struct {
	elems []pathElem
}

// Field returns a copy of the path with an object-field step appended.
func (p VariantPath) Field(name string) VariantPath {
	return VariantPath{elems: append(p.grow(), pathElem{name: name, isField: true})}
}

// Index returns a copy of the path with an array-index step appended.
func (p VariantPath) Index(i int) VariantPath {
	return VariantPath{elems: append(p.grow(), pathElem{index: i})}
}

// Join returns a copy of the path with other's steps appended.
func (p VariantPath) Join(other VariantPath) VariantPath {
	return VariantPath{elems: append(p.grow(), other.elems...)}
}

func (p VariantPath) grow() []pathElem {
	return append(make([]pathElem, 0, len(p.elems)+1), p.elems...)
}

// Len returns the number of steps in the path.
func (p VariantPath) Len() int { return len(p.elems) }

// StepAt returns the i-th step's name and index, with isField true for an object-field step.
func (p VariantPath) StepAt(i int) (name string, index int, isField bool) {
	e := p.elems[i]

	return e.name, e.index, e.isField
}

// GetByPath navigates path into v and returns the leaf value. found is false when
// the path is cleanly absent (a missing object field, or an out-of-range or
// non-array index). It returns an error for a type error (a field step into a
// non-object) or corrupt data (a field id not present in the metadata).
func (v Value) GetByPath(path VariantPath) (leaf Value, found bool, err error) {
	cur := v
	for _, e := range path.elems {
		if e.isField {
			obj, ok := cur.Value().(ObjectValue)
			if !ok {
				return Value{}, false, fmt.Errorf("%w: variant path field %q applied to non-object", arrow.ErrInvalid, e.name)
			}
			field, ferr := obj.ValueByKey(e.name)
			if ferr != nil {
				if errors.Is(ferr, arrow.ErrNotFound) {
					return Value{}, false, nil
				}

				return Value{}, false, ferr
			}
			cur = field.Value

			continue
		}

		arr, ok := cur.Value().(ArrayValue)
		if !ok {
			return Value{}, false, nil
		}
		if e.index < 0 || uint64(e.index) >= uint64(arr.Len()) {
			return Value{}, false, nil
		}
		el, aerr := arr.Value(uint32(e.index))
		if aerr != nil {
			return Value{}, false, nil
		}
		cur = el
	}

	return cur, true, nil
}
