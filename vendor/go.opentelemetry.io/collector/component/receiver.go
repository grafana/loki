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

package component // import "go.opentelemetry.io/collector/component"

import (
	"context"

	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer"
)

// Receiver allows the collector to receive metrics, traces and logs.
//
// Receiver receives data from a source (either from a remote source via network
// or scrapes from a local host) and pushes the data to the pipelines it is attached
// to by calling the nextConsumer.Consume*() function.
//
// Error Handling
// ==============
// The nextConsumer.Consume*() function may return an error to indicate that the data
// was not accepted. There are 2 types of possible errors: Permanent and non-Permanent.
// The receiver must check the type of the error using IsPermanent() helper.
//
// If the error is Permanent, then the nextConsumer.Consume*() call should not be
// retried with the same data. This typically happens when the data cannot be
// serialized by the exporter that is attached to the pipeline or when the destination
// refuses the data because it cannot decode it. The receiver must indicate to
// the source from which it received the data that the received data was bad, if the
// receiving protocol allows to do that. In case of OTLP/HTTP for example, this means
// that HTTP 400 response is returned to the sender.
//
// If the error is non-Permanent then the nextConsumer.Consume*() call may be retried
// with the same data. This may be done by the receiver itself, however typically it is
// done by the original sender, after the receiver returns a response to the sender
// indicating that the Collector is currently overloaded and the request must be
// retried. In case of OTLP/HTTP for example, this means that HTTP 429 or 503 response
// is returned.
//
// Acknowledgment Handling
// =======================
// The receivers that receive data via a network protocol that support acknowledgments
// MUST follow this order of operations:
// - Receive data from some sender (typically from a network).
// - Push received data to the pipeline by calling nextConsumer.Consume*() function.
// - Acknowledge successful data receipt to the sender if Consume*() succeeded or
//   return a failure to the sender if Consume*() returned an error.
// This ensures there are strong delivery guarantees once the data is acknowledged
// by the Collector.
type Receiver interface {
	Component
}

// A TracesReceiver receives traces.
// Its purpose is to translate data from any format to the collector's internal trace format.
// TracesReceiver feeds a consumer.Traces with data.
//
// For example it could be Zipkin data source which translates Zipkin spans into pdata.Traces.
type TracesReceiver interface {
	Receiver
}

// A MetricsReceiver receives metrics.
// Its purpose is to translate data from any format to the collector's internal metrics format.
// MetricsReceiver feeds a consumer.Metrics with data.
//
// For example it could be Prometheus data source which translates Prometheus metrics into pdata.Metrics.
type MetricsReceiver interface {
	Receiver
}

// A LogsReceiver receives logs.
// Its purpose is to translate data from any format to the collector's internal logs data format.
// LogsReceiver feeds a consumer.Logs with data.
//
// For example a LogsReceiver can read syslogs and convert them into pdata.Logs.
type LogsReceiver interface {
	Receiver
}

// ReceiverCreateSettings configures Receiver creators.
type ReceiverCreateSettings struct {
	TelemetrySettings

	// BuildInfo can be used by components for informational purposes.
	BuildInfo BuildInfo
}

// ReceiverFactory can create TracesReceiver, MetricsReceiver and
// and LogsReceiver. This is the new preferred factory type to create receivers.
//
// This interface cannot be directly implemented. Implementations must
// use the receiverhelper.NewFactory to implement it.
type ReceiverFactory interface {
	Factory

	// CreateDefaultConfig creates the default configuration for the Receiver.
	// This method can be called multiple times depending on the pipeline
	// configuration and should not cause side-effects that prevent the creation
	// of multiple instances of the Receiver.
	// The object returned by this method needs to pass the checks implemented by
	// 'configtest.CheckConfigStruct'. It is recommended to have these checks in the
	// tests of any implementation of the Factory interface.
	CreateDefaultConfig() config.Receiver

	// CreateTracesReceiver creates a trace receiver based on this config.
	// If the receiver type does not support tracing or if the config is not valid
	// an error will be returned instead.
	CreateTracesReceiver(ctx context.Context, set ReceiverCreateSettings,
		cfg config.Receiver, nextConsumer consumer.Traces) (TracesReceiver, error)

	// CreateMetricsReceiver creates a metrics receiver based on this config.
	// If the receiver type does not support metrics or if the config is not valid
	// an error will be returned instead.
	CreateMetricsReceiver(ctx context.Context, set ReceiverCreateSettings,
		cfg config.Receiver, nextConsumer consumer.Metrics) (MetricsReceiver, error)

	// CreateLogsReceiver creates a log receiver based on this config.
	// If the receiver type does not support the data type or if the config is not valid
	// an error will be returned instead.
	CreateLogsReceiver(ctx context.Context, set ReceiverCreateSettings,
		cfg config.Receiver, nextConsumer consumer.Logs) (LogsReceiver, error)
}
