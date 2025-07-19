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

//go:build !localvalidationscheme

package prometheus

import "github.com/prometheus/common/model"

// ExemplarAdder is implemented by Counters that offer the option of adding a
// value to the Counter together with an exemplar. Its AddWithExemplar method
// works like the Add method of the Counter interface but also replaces the
// currently saved exemplar (if any) with a new one, created from the provided
// value, the current time as timestamp, and the provided labels. Empty Labels
// will lead to a valid (label-less) exemplar. But if Labels is nil, the current
// exemplar is left in place. AddWithExemplar panics if the value is < 0, if any
// of the provided labels are invalid, or if the provided labels contain more
// than 128 runes in total.
type ExemplarAdder interface {
	AddWithExemplar(value float64, exemplar Labels)
}

func (c *counter) AddWithExemplar(v float64, e Labels) {
	c.Add(v)
	//nolint:staticcheck // model.NameValidationScheme is being phased out.
	c.updateExemplar(v, e, model.NameValidationScheme)
}
