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
	"time"

	"github.com/prometheus/common/model"
)

// ObserveDurationWithExemplar is like ObserveDuration, but it will also
// observe exemplar with the duration unless exemplar is nil or provided Observer can't
// be casted to ExemplarObserver.
func (t *Timer) ObserveDurationWithExemplar(exemplar Labels, scheme model.ValidationScheme) time.Duration {
	d := time.Since(t.begin)
	eo, ok := t.observer.(ExemplarObserver)
	if ok && exemplar != nil {
		eo.ObserveWithExemplar(d.Seconds(), exemplar, scheme)
		return d
	}
	if t.observer != nil {
		t.observer.Observe(d.Seconds())
	}
	return d
}
