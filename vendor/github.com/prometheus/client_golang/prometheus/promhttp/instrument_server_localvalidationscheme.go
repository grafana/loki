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

package promhttp

import (
	"github.com/prometheus/common/model"

	"github.com/prometheus/client_golang/prometheus"
)

// observeWithExemplar is a wrapper for [prometheus.ExemplarAdder.ExemplarObserver],
// which falls back to [prometheus.Observer.Observe] if no labels are provided.
func observeWithExemplar(obs prometheus.Observer, val float64, labels map[string]string, scheme model.ValidationScheme) {
	if scheme == model.UnsetValidation {
		scheme = model.UTF8Validation
	}
	if labels == nil {
		obs.Observe(val)
		return
	}
	obs.(prometheus.ExemplarObserver).ObserveWithExemplar(val, labels, scheme)
}

// addWithExemplar is a wrapper for [prometheus.ExemplarAdder.AddWithExemplar],
// which falls back to [prometheus.Counter.Add] if no labels are provided.
func addWithExemplar(obs prometheus.Counter, val float64, labels map[string]string, scheme model.ValidationScheme) {
	if labels == nil {
		obs.Add(val)
		return
	}
	obs.(prometheus.ExemplarAdder).AddWithExemplar(val, labels, scheme)
}
