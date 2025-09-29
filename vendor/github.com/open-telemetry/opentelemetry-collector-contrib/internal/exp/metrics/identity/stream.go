// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package identity // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"

import (
	"fmt"
	"hash"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil"
)

type Stream struct {
	metric Metric
	attrs  [16]byte
}

func (s Stream) Hash() hash.Hash64 {
	sum := s.metric.Hash()
	sum.Write(s.attrs[:])
	return sum
}

func (s Stream) Metric() Metric {
	return s.metric
}

func (s Stream) String() string {
	return fmt.Sprintf("stream/%x", s.Hash().Sum64())
}

func OfStream[DataPoint attrPoint](m Metric, dp DataPoint) Stream {
	return Stream{metric: m, attrs: pdatautil.MapHash(dp.Attributes())}
}

type attrPoint interface {
	Attributes() pcommon.Map
}
