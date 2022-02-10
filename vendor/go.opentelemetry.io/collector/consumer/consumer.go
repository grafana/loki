// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package consumer // import "go.opentelemetry.io/collector/consumer"

import (
	"context"

	"go.opentelemetry.io/collector/model/pdata"
)

// Capabilities describes the capabilities of a Processor.
type Capabilities struct {
	// MutatesData is set to true if Consume* function of the
	// processor modifies the input TraceData or MetricsData argument.
	// Processors which modify the input data MUST set this flag to true. If the processor
	// does not modify the data it MUST set this flag to false. If the processor creates
	// a copy of the data before modifying then this flag can be safely set to false.
	MutatesData bool
}

type baseConsumer interface {
	Capabilities() Capabilities
}

// Metrics is the new metrics consumer interface that receives pdata.Metrics, processes it
// as needed, and sends it to the next processing node if any or to the destination.
type Metrics interface {
	baseConsumer
	// ConsumeMetrics receives pdata.Metrics for consumption.
	ConsumeMetrics(ctx context.Context, md pdata.Metrics) error
}

// Traces is an interface that receives pdata.Traces, processes it
// as needed, and sends it to the next processing node if any or to the destination.
type Traces interface {
	baseConsumer
	// ConsumeTraces receives pdata.Traces for consumption.
	ConsumeTraces(ctx context.Context, td pdata.Traces) error
}

// Logs is an interface that receives pdata.Logs, processes it
// as needed, and sends it to the next processing node if any or to the destination.
type Logs interface {
	baseConsumer
	// ConsumeLogs receives pdata.Logs for consumption.
	ConsumeLogs(ctx context.Context, ld pdata.Logs) error
}
