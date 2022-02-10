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

package logs // import "go.opentelemetry.io/collector/receiver/otlpreceiver/internal/logs"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/otlpgrpc"
	"go.opentelemetry.io/collector/obsreport"
)

const (
	dataFormatProtobuf = "protobuf"
	receiverTransport  = "grpc"
)

// Receiver is the type used to handle spans from OpenTelemetry exporters.
type Receiver struct {
	nextConsumer consumer.Logs
	obsrecv      *obsreport.Receiver
}

// New creates a new Receiver reference.
func New(id config.ComponentID, nextConsumer consumer.Logs, set component.ReceiverCreateSettings) *Receiver {
	return &Receiver{
		nextConsumer: nextConsumer,
		obsrecv: obsreport.NewReceiver(obsreport.ReceiverSettings{
			ReceiverID:             id,
			Transport:              receiverTransport,
			ReceiverCreateSettings: set,
		}),
	}
}

// Export implements the service Export logs func.
func (r *Receiver) Export(ctx context.Context, req otlpgrpc.LogsRequest) (otlpgrpc.LogsResponse, error) {
	ld := req.Logs()
	numSpans := ld.LogRecordCount()
	if numSpans == 0 {
		return otlpgrpc.NewLogsResponse(), nil
	}

	ctx = r.obsrecv.StartLogsOp(ctx)
	err := r.nextConsumer.ConsumeLogs(ctx, ld)
	r.obsrecv.EndLogsOp(ctx, dataFormatProtobuf, numSpans, err)

	return otlpgrpc.NewLogsResponse(), err
}
