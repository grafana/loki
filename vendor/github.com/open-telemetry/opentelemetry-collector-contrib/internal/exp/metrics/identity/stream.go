// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package identity // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"

import (
	"hash"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil"
)

type Stream struct {
	metric
	attrs [16]byte
}

func (i Stream) Hash() hash.Hash64 {
	sum := i.metric.Hash()
	sum.Write(i.attrs[:])
	return sum
}

func (i Stream) Metric() Metric {
	return i.metric
}

func OfStream[DataPoint attrPoint](m Metric, dp DataPoint) Stream {
	return Stream{metric: m, attrs: pdatautil.MapHash(dp.Attributes())}
}

type attrPoint interface {
	Attributes() pcommon.Map
}
