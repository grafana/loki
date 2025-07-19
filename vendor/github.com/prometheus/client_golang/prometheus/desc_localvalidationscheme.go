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

type descriptorOptions struct {
	validationScheme model.ValidationScheme
}

// newDescriptorOptions creates default descriptor options and applies opts.
func newDescriptorOptions(opts ...DescOption) *descriptorOptions {
	d := &descriptorOptions{
		validationScheme: model.UTF8Validation,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// WithValidationScheme ensures descriptor's label and metric names adhere to scheme.
// Default is UTF-8 validation.
func WithValidationScheme(scheme model.ValidationScheme) DescOption {
	return func(o *descriptorOptions) {
		o.validationScheme = scheme
	}
}

// DescOption are options that can be passed to NewDesc
type DescOption func(*descriptorOptions)

// NewDesc allocates and initializes a new Desc. Errors are recorded in the Desc
// and will be reported on registration time. variableLabels and constLabels can
// be nil if no such labels should be set. fqName must not be empty.
//
// variableLabels only contain the label names. Their label values are variable
// and therefore not part of the Desc. (They are managed within the Metric.)
//
// For constLabels, the label values are constant. Therefore, they are fully
// specified in the Desc. See the Collector example for a usage pattern.
func NewDesc(fqName, help string, variableLabels []string, constLabels Labels, opts ...DescOption) *Desc {
	return V2.NewDesc(fqName, help, UnconstrainedLabels(variableLabels), constLabels, opts...)
}

func (v2) NewDesc(fqName, help string, variableLabels ConstrainableLabels, constLabels Labels, opts ...DescOption) *Desc {
	descOpts := newDescriptorOptions(opts...)
	return newDesc(fqName, help, variableLabels, constLabels, descOpts.validationScheme)
}

func isValidMetricName(metricName model.LabelValue, scheme model.ValidationScheme) bool {
	return model.IsValidMetricName(metricName, scheme)
}
