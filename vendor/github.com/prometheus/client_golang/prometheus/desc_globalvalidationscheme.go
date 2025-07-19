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

import (
	"github.com/prometheus/common/model"
)

// NewDesc allocates and initializes a new Desc. Errors are recorded in the Desc
// and will be reported on registration time. variableLabels and constLabels can
// be nil if no such labels should be set. fqName must not be empty.
//
// variableLabels only contain the label names. Their label values are variable
// and therefore not part of the Desc. (They are managed within the Metric.)
//
// For constLabels, the label values are constant. Therefore, they are fully
// specified in the Desc. See the Collector example for a usage pattern.
func NewDesc(fqName, help string, variableLabels []string, constLabels Labels) *Desc {
	return V2.NewDesc(fqName, help, UnconstrainedLabels(variableLabels), constLabels)
}

// NewDesc allocates and initializes a new Desc. Errors are recorded in the Desc
// and will be reported on registration time. variableLabels and constLabels can
// be nil if no such labels should be set. fqName must not be empty.
//
// variableLabels only contain the label names and normalization functions. Their
// label values are variable and therefore not part of the Desc. (They are managed
// within the Metric.)
//
// For constLabels, the label values are constant. Therefore, they are fully
// specified in the Desc. See the Collector example for a usage pattern.
func (v2) NewDesc(fqName, help string, variableLabels ConstrainableLabels, constLabels Labels) *Desc {
	//nolint:staticcheck // model.NameValidationScheme is being phased out.
	return newDesc(fqName, help, variableLabels, constLabels, model.NameValidationScheme)
}

func isValidMetricName(metricName model.LabelValue, _ model.ValidationScheme) bool {
	return model.IsValidMetricName(metricName)
}
