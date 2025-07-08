// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build localvalidationscheme

package model

import (
	"encoding/json"
	"fmt"
)

// Validate checks whether all names and values in the label set
// are valid.
func (ls LabelSet) Validate(scheme ValidationScheme) error {
	return ls.validate(scheme)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// Validates label names using UTF8Validation.
func (l *LabelSet) UnmarshalJSON(b []byte) error {
	var m map[LabelName]LabelValue
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	// encoding/json only unmarshals maps of the form map[string]T. It treats
	// LabelName as a string and does not call its UnmarshalJSON method.
	// Thus, we have to replicate the behavior here.
	for ln := range m {
		if !ln.IsValid(UTF8Validation) {
			return fmt.Errorf("%q is not a valid label name", ln)
		}
	}
	*l = LabelSet(m)
	return nil
}
