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

// IsValid returns true iff the name matches the pattern of LabelNameRE when
// scheme is LegacyValidation, or valid UTF-8 if it is UTF8Validation.
func (ln LabelName) IsValid(validationScheme ValidationScheme) bool {
	return ln.isValid(validationScheme)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
// Validation is done using UTF8Validation.
func (ln *LabelName) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if !LabelName(s).IsValid(UTF8Validation) {
		return fmt.Errorf("%q is not a valid label name", s)
	}
	*ln = LabelName(s)
	return nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// Validation is done using UTF8Validation.
func (ln *LabelName) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if !LabelName(s).IsValid(UTF8Validation) {
		return fmt.Errorf("%q is not a valid label name", s)
	}
	*ln = LabelName(s)
	return nil
}
