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

import "github.com/prometheus/common/model"

// ObserveWithExemplar should not be called in a high-frequency setting
// for a native histogram with configured exemplars. For this case,
// the implementation isn't lock-free and might suffer from lock contention.
func (h *histogram) ObserveWithExemplar(v float64, e Labels, scheme model.ValidationScheme) {
	i := h.findBucket(v)
	h.observe(v, i)
	h.updateExemplar(v, i, e, scheme)
}
