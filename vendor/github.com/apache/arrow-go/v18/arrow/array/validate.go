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

package array

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// Validator is implemented by array types that can validate their internal
// consistency. See Validate and ValidateFull for top-level dispatch.
type Validator interface {
	arrow.Array
	// Validate performs a basic O(1) consistency check.
	Validate() error
	// ValidateFull performs a thorough O(n) consistency check.
	ValidateFull() error
}

// Validate performs a basic O(1) consistency check on arr, returning an error
// if the array's internal buffers are inconsistent. For array types that do not
// implement Validator, nil is returned.
//
// Use this to detect corrupted data from untrusted sources such as Arrow Flight
// or Flight SQL servers before accessing values, which may otherwise panic.
func Validate(arr arrow.Array) error {
	if v, ok := arr.(Validator); ok {
		return v.Validate()
	}
	return nil
}

// ValidateFull performs a thorough O(n) consistency check on arr, returning an
// error if the array's internal buffers are inconsistent. For array types that
// do not implement Validator, nil is returned.
//
// Unlike Validate, this checks every element and is therefore O(n). Use this
// when receiving data from untrusted sources where subtle corruption (e.g.
// non-monotonic offsets) may not be detected by Validate alone.
func ValidateFull(arr arrow.Array) error {
	if v, ok := arr.(Validator); ok {
		return v.ValidateFull()
	}
	return nil
}

// ValidateRecord validates each column in rec using Validate, returning the
// first error encountered. The error includes the column index and field name.
func ValidateRecord(rec arrow.RecordBatch) error {
	for i := int64(0); i < rec.NumCols(); i++ {
		if err := Validate(rec.Column(int(i))); err != nil {
			return fmt.Errorf("column %d (%s): %w", i, rec.Schema().Field(int(i)).Name, err)
		}
	}
	return nil
}

// ValidateRecordFull validates each column in rec using ValidateFull, returning
// the first error encountered. The error includes the column index and field name.
func ValidateRecordFull(rec arrow.RecordBatch) error {
	for i := int64(0); i < rec.NumCols(); i++ {
		if err := ValidateFull(rec.Column(int(i))); err != nil {
			return fmt.Errorf("column %d (%s): %w", i, rec.Schema().Field(int(i)).Name, err)
		}
	}
	return nil
}
