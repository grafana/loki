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

package prometheus

import (
	"github.com/prometheus/common/model"
)

// NewMetricWithExemplars returns a new Metric wrapping the provided Metric with given
// exemplars. Exemplars are validated.
//
// Only last applicable exemplar is injected from the list.
// For example for Counter it means last exemplar is injected.
// For Histogram, it means last applicable exemplar for each bucket is injected.
// For a Native Histogram, all valid exemplars are injected.
//
// NewMetricWithExemplars works best with MustNewConstMetric and
// MustNewConstHistogram, see example.
func NewMetricWithExemplars(m Metric, scheme model.ValidationScheme, exemplars ...Exemplar) (Metric, error) {
	return newMetricWithExemplars(m, scheme, exemplars...)
}

// MustNewMetricWithExemplars is a version of NewMetricWithExemplars that panics where
// NewMetricWithExemplars would have returned an error.
func MustNewMetricWithExemplars(m Metric, scheme model.ValidationScheme, exemplars ...Exemplar) Metric {
	ret, err := NewMetricWithExemplars(m, scheme, exemplars...)
	if err != nil {
		panic(err)
	}
	return ret
}
