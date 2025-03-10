// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package data // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data"

import (
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"
)

type Number struct {
	pmetric.NumberDataPoint
}

type Histogram struct {
	pmetric.HistogramDataPoint
}

type ExpHistogram struct {
	expo.DataPoint
}

type Summary struct {
	pmetric.SummaryDataPoint
}
