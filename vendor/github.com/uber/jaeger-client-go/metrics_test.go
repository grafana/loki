// Copyright (c) 2017 Uber Technologies, Inc.
//
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

package jaeger

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-lib/metrics"
	"github.com/uber/jaeger-lib/metrics/testutils"
)

func TestNewMetrics(t *testing.T) {
	factory := metrics.NewLocalFactory(0)
	m := NewMetrics(factory, map[string]string{"lib": "jaeger"})

	require.NotNil(t, m.SpansStartedSampled, "counter not initialized")
	require.NotNil(t, m.ReporterQueueLength, "gauge not initialized")

	m.SpansStartedSampled.Inc(1)
	m.ReporterQueueLength.Update(11)
	testutils.AssertCounterMetrics(t, factory,
		testutils.ExpectedMetric{
			Name:  "jaeger.started_spans",
			Tags:  map[string]string{"lib": "jaeger", "sampled": "y"},
			Value: 1,
		},
	)
	testutils.AssertGaugeMetrics(t, factory,
		testutils.ExpectedMetric{
			Name:  "jaeger.reporter_queue_length",
			Tags:  map[string]string{"lib": "jaeger"},
			Value: 11,
		},
	)
}
