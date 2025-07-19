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
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
)

type RegistryOption struct {
	NameValidationScheme model.ValidationScheme
}

// NewRegistry creates a new vanilla Registry without any Collectors
// pre-registered.
func NewRegistry(opts ...RegistryOption) *Registry {
	scheme := model.UTF8Validation
	for _, opt := range opts {
		if opt.NameValidationScheme != model.UnsetValidation {
			scheme = opt.NameValidationScheme
		}
	}
	return &Registry{
		collectorsByID:       map[uint64]Collector{},
		descIDs:              map[uint64]struct{}{},
		dimHashesByName:      map[string]uint64{},
		nameValidationScheme: scheme,
	}
}

// NewPedanticRegistry returns a registry that checks during collection if each
// collected Metric is consistent with its reported Desc, and if the Desc has
// actually been registered with the registry. Unchecked Collectors (those whose
// Describe method does not yield any descriptors) are excluded from the check.
//
// Usually, a Registry will be happy as long as the union of all collected
// Metrics is consistent and valid even if some metrics are not consistent with
// their own Desc or a Desc provided by their registered Collector. Well-behaved
// Collectors and Metrics will only provide consistent Descs. This Registry is
// useful to test the implementation of Collectors and Metrics.
func NewPedanticRegistry(opts ...RegistryOption) *Registry {
	r := NewRegistry(opts...)
	r.pedanticChecksEnabled = true
	return r
}

// Gatherers is a collection of Gatherer instances that implements the Gatherer
// interface itself. Its Gather method calls Gather on all Gatherers in the
// slice in order and returns the merged results. Errors returned from the
// Gather calls are all returned in a flattened MultiError. Duplicate and
// inconsistent Metrics are skipped (first occurrence in slice order wins) and
// reported in the returned error.
//
// Gatherers can be used to merge the Gather results from multiple
// Registries. It also provides a way to directly inject existing MetricFamily
// protobufs into the gathering by creating a custom Gatherer with a Gather
// method that simply returns the existing MetricFamily protobufs. Note that no
// registration is involved (in contrast to Collector registration), so
// obviously registration-time checks cannot happen. Any inconsistencies between
// the gathered MetricFamilies are reported as errors by the Gather method, and
// inconsistent Metrics are dropped. Invalid parts of the MetricFamilies
// (e.g. syntactically invalid metric or label names) will go undetected.
type Gatherers struct {
	gatherers        []Gatherer
	validationScheme model.ValidationScheme
}

// NewGatherers returns a new Gatherers encapsulating the provided gatherers.
func NewGatherers(gatherers []Gatherer, validationScheme model.ValidationScheme) Gatherers {
	return Gatherers{
		gatherers:        gatherers,
		validationScheme: validationScheme,
	}
}

// Gather implements Gatherer.
func (gs Gatherers) Gather() ([]*dto.MetricFamily, error) {
	return gs.gather(gs.gatherers, gs.validationScheme)
}

func (gs *Gatherers) AddGatherer(g Gatherer) {
	gs.gatherers = append(gs.gatherers, g)
}

// Range calls f for every Gatherer in gs.
func (gs *Gatherers) Range(f func(Gatherer)) {
	for _, g := range gs.gatherers {
		f(g)
	}
}
