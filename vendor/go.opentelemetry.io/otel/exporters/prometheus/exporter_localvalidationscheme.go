// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build localvalidationscheme

package prometheus // import "go.opentelemetry.io/otel/exporters/prometheus"

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

func newMetricWithExemplars(m prometheus.Metric, scheme model.ValidationScheme, exemplars ...prometheus.Exemplar) (prometheus.Metric, error) {
	return prometheus.NewMetricWithExemplars(m, scheme, exemplars...)
}
