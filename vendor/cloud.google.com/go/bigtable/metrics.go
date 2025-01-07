/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bigtable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/bigtable/internal"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/api/option"
)

const (
	builtInMetricsMeterName = "bigtable.googleapis.com/internal/client/"

	metricsPrefix         = "bigtable/"
	locationMDKey         = "x-goog-ext-425905942-bin"
	serverTimingMDKey     = "server-timing"
	serverTimingValPrefix = "gfet4t7; dur="

	// Monitored resource labels
	monitoredResLabelKeyProject  = "project_id"
	monitoredResLabelKeyInstance = "instance"
	monitoredResLabelKeyTable    = "table"
	monitoredResLabelKeyCluster  = "cluster"
	monitoredResLabelKeyZone     = "zone"

	// Metric labels
	metricLabelKeyAppProfile         = "app_profile"
	metricLabelKeyMethod             = "method"
	metricLabelKeyStatus             = "status"
	metricLabelKeyTag                = "tag"
	metricLabelKeyStreamingOperation = "streaming"
	metricLabelKeyClientName         = "client_name"
	metricLabelKeyClientUID          = "client_uid"

	// Metric names
	metricNameOperationLatencies = "operation_latencies"
	metricNameAttemptLatencies   = "attempt_latencies"
	metricNameServerLatencies    = "server_latencies"
	metricNameRetryCount         = "retry_count"
	metricNameDebugTags          = "debug_tags"

	// Metric units
	metricUnitMS    = "ms"
	metricUnitCount = "1"
)

// These are effectively constant, but for testing purposes they are mutable
var (
	// duration between two metric exports
	defaultSamplePeriod = time.Minute

	metricsErrorPrefix = "bigtable-metrics: "

	clientName = fmt.Sprintf("go-bigtable/%v", internal.Version)

	bucketBounds = []float64{0.0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 8.0, 10.0, 13.0, 16.0, 20.0, 25.0, 30.0, 40.0,
		50.0, 65.0, 80.0, 100.0, 130.0, 160.0, 200.0, 250.0, 300.0, 400.0, 500.0, 650.0,
		800.0, 1000.0, 2000.0, 5000.0, 10000.0, 20000.0, 50000.0, 100000.0, 200000.0,
		400000.0, 800000.0, 1600000.0, 3200000.0}

	// All the built-in metrics have same attributes except 'tag', 'status' and 'streaming'
	// These attributes need to be added to only few of the metrics
	metricsDetails = map[string]metricInfo{
		metricNameOperationLatencies: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
				metricLabelKeyStreamingOperation,
			},
			recordedPerAttempt: false,
		},
		metricNameAttemptLatencies: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
				metricLabelKeyStreamingOperation,
			},
			recordedPerAttempt: true,
		},
		metricNameServerLatencies: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
				metricLabelKeyStreamingOperation,
			},
			recordedPerAttempt: true,
		},
		metricNameRetryCount: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
			},
			recordedPerAttempt: true,
		},
	}

	// Generates unique client ID in the format go-<random UUID>@<hostname>
	generateClientUID = func() (string, error) {
		hostname := "localhost"
		hostname, err := os.Hostname()
		if err != nil {
			return "", err
		}
		return "go-" + uuid.NewString() + "@" + hostname, nil
	}

	// GCM exporter should use the same options as Bigtable client
	// createExporterOptions takes Bigtable client options and returns exporter options
	// Overwritten in tests
	createExporterOptions = func(btOpts ...option.ClientOption) []option.ClientOption {
		return btOpts
	}
)

type metricInfo struct {
	additionalAttrs    []string
	recordedPerAttempt bool
}

type builtinMetricsTracerFactory struct {
	enabled bool

	// To be called on client close
	shutdown func()

	// attributes that are specific to a client instance and
	// do not change across different function calls on client
	clientAttributes []attribute.KeyValue

	operationLatencies metric.Float64Histogram
	serverLatencies    metric.Float64Histogram
	attemptLatencies   metric.Float64Histogram
	retryCount         metric.Int64Counter
	debugTags          metric.Int64Counter
}

func newBuiltinMetricsTracerFactory(ctx context.Context, project, instance, appProfile string, metricsProvider MetricsProvider, opts ...option.ClientOption) (*builtinMetricsTracerFactory, error) {
	clientUID, err := generateClientUID()
	if err != nil {
		return nil, err
	}

	tracerFactory := &builtinMetricsTracerFactory{
		enabled: false,
		clientAttributes: []attribute.KeyValue{
			attribute.String(monitoredResLabelKeyProject, project),
			attribute.String(monitoredResLabelKeyInstance, instance),
			attribute.String(metricLabelKeyAppProfile, appProfile),
			attribute.String(metricLabelKeyClientUID, clientUID),
			attribute.String(metricLabelKeyClientName, clientName),
		},
		shutdown: func() {},
	}

	var meterProvider *sdkmetric.MeterProvider
	if metricsProvider == nil {
		// Create default meter provider
		mpOptions, err := builtInMeterProviderOptions(project, opts...)
		if err != nil {
			return tracerFactory, err
		}
		meterProvider = sdkmetric.NewMeterProvider(mpOptions...)

		tracerFactory.enabled = true
		tracerFactory.shutdown = func() { meterProvider.Shutdown(ctx) }
	} else {
		switch metricsProvider.(type) {
		case NoopMetricsProvider:
			tracerFactory.enabled = false
			return tracerFactory, nil
		default:
			tracerFactory.enabled = false
			return tracerFactory, errors.New("unknown MetricsProvider type")
		}
	}

	// Create meter and instruments
	meter := meterProvider.Meter(builtInMetricsMeterName, metric.WithInstrumentationVersion(internal.Version))
	err = tracerFactory.createInstruments(meter)
	return tracerFactory, err
}

func builtInMeterProviderOptions(project string, opts ...option.ClientOption) ([]sdkmetric.Option, error) {
	allOpts := createExporterOptions(opts...)
	defaultExporter, err := newMonitoringExporter(context.Background(), project, allOpts...)
	if err != nil {
		return nil, err
	}

	return []sdkmetric.Option{sdkmetric.WithReader(
		sdkmetric.NewPeriodicReader(
			defaultExporter,
			sdkmetric.WithInterval(defaultSamplePeriod),
		),
	)}, nil
}

func (tf *builtinMetricsTracerFactory) createInstruments(meter metric.Meter) error {
	var err error

	// Create operation_latencies
	tf.operationLatencies, err = meter.Float64Histogram(
		metricNameOperationLatencies,
		metric.WithDescription("Total time until final operation success or failure, including retries and backoff."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create attempt_latencies
	tf.attemptLatencies, err = meter.Float64Histogram(
		metricNameAttemptLatencies,
		metric.WithDescription("Client observed latency per RPC attempt."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create server_latencies
	tf.serverLatencies, err = meter.Float64Histogram(
		metricNameServerLatencies,
		metric.WithDescription("The latency measured from the moment that the RPC entered the Google data center until the RPC was completed."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create retry_count
	tf.retryCount, err = meter.Int64Counter(
		metricNameRetryCount,
		metric.WithDescription("The number of additional RPCs sent after the initial attempt."),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return err
	}

	// Create debug_tags
	tf.debugTags, err = meter.Int64Counter(
		metricNameDebugTags,
		metric.WithDescription("A counter of internal client events used for debugging."),
		metric.WithUnit(metricUnitCount),
	)
	return err
}

// builtinMetricsTracer is created one per operation
// It is used to store metric instruments, attribute values
// and other data required to obtain and record them
type builtinMetricsTracer struct {
	ctx            context.Context
	builtInEnabled bool

	// attributes that are specific to a client instance and
	// do not change across different operations on client
	clientAttributes []attribute.KeyValue

	instrumentOperationLatencies metric.Float64Histogram
	instrumentServerLatencies    metric.Float64Histogram
	instrumentAttemptLatencies   metric.Float64Histogram
	instrumentRetryCount         metric.Int64Counter
	instrumentDebugTags          metric.Int64Counter

	tableName   string
	method      string
	isStreaming bool

	currOp opTracer
}

func (b *builtinMetricsTracer) setMethod(m string) {
	b.method = "Bigtable." + m
}

// opTracer is used to record metrics for the entire operation, including retries.
// Operation is a logical unit that represents a single method invocation on client.
// The method might require multiple attempts/rpcs and backoff logic to complete
type opTracer struct {
	attemptCount int64

	startTime time.Time

	// gRPC status code of last completed attempt
	status string

	currAttempt attemptTracer
}

func (o *opTracer) setStartTime(t time.Time) {
	o.startTime = t
}

func (o *opTracer) setStatus(status string) {
	o.status = status
}

func (o *opTracer) incrementAttemptCount() {
	o.attemptCount++
}

// attemptTracer is used to record metrics for each individual attempt of the operation.
// Attempt corresponds to an attempt of an RPC.
type attemptTracer struct {
	startTime time.Time
	clusterID string
	zoneID    string

	// gRPC status code
	status string

	// Server latency in ms
	serverLatency float64

	// Error seen while getting server latency from headers
	serverLatencyErr error
}

func (a *attemptTracer) setStartTime(t time.Time) {
	a.startTime = t
}

func (a *attemptTracer) setClusterID(clusterID string) {
	a.clusterID = clusterID
}

func (a *attemptTracer) setZoneID(zoneID string) {
	a.zoneID = zoneID
}

func (a *attemptTracer) setStatus(status string) {
	a.status = status
}

func (a *attemptTracer) setServerLatency(latency float64) {
	a.serverLatency = latency
}

func (a *attemptTracer) setServerLatencyErr(err error) {
	a.serverLatencyErr = err
}

func (tf *builtinMetricsTracerFactory) createBuiltinMetricsTracer(ctx context.Context, tableName string, isStreaming bool) builtinMetricsTracer {
	// Operation has started but not the attempt.
	// So, create only operation tracer and not attempt tracer
	currOpTracer := opTracer{}
	currOpTracer.setStartTime(time.Now())

	return builtinMetricsTracer{
		ctx:            ctx,
		builtInEnabled: tf.enabled,

		currOp:           currOpTracer,
		clientAttributes: tf.clientAttributes,

		instrumentOperationLatencies: tf.operationLatencies,
		instrumentServerLatencies:    tf.serverLatencies,
		instrumentAttemptLatencies:   tf.attemptLatencies,
		instrumentRetryCount:         tf.retryCount,
		instrumentDebugTags:          tf.debugTags,

		tableName:   tableName,
		isStreaming: isStreaming,
	}
}

// toOtelMetricAttrs:
// - converts metric attributes values captured throughout the operation / attempt
// to OpenTelemetry attributes format,
// - combines these with common client attributes and returns
func (mt *builtinMetricsTracer) toOtelMetricAttrs(metricName string) ([]attribute.KeyValue, error) {
	// Create attribute key value pairs for attributes common to all metricss
	attrKeyValues := []attribute.KeyValue{
		attribute.String(metricLabelKeyMethod, mt.method),

		// Add resource labels to otel metric labels.
		// These will be used for creating the monitored resource but exporter
		// will not add them to Google Cloud Monitoring metric labels
		attribute.String(monitoredResLabelKeyTable, mt.tableName),

		// Irrespective of whether metric is attempt specific or operation specific,
		// use last attempt's cluster and zone
		attribute.String(monitoredResLabelKeyCluster, mt.currOp.currAttempt.clusterID),
		attribute.String(monitoredResLabelKeyZone, mt.currOp.currAttempt.zoneID),
	}
	attrKeyValues = append(attrKeyValues, mt.clientAttributes...)

	// Get metric details
	mDetails, found := metricsDetails[metricName]
	if !found {
		return attrKeyValues, fmt.Errorf("unable to create attributes list for unknown metric: %v", metricName)
	}

	status := mt.currOp.status
	if mDetails.recordedPerAttempt {
		status = mt.currOp.currAttempt.status
	}

	// Add additional attributes to metrics
	for _, attrKey := range mDetails.additionalAttrs {
		switch attrKey {
		case metricLabelKeyStatus:
			attrKeyValues = append(attrKeyValues, attribute.String(metricLabelKeyStatus, status))
		case metricLabelKeyStreamingOperation:
			attrKeyValues = append(attrKeyValues, attribute.Bool(metricLabelKeyStreamingOperation, mt.isStreaming))
		default:
			return attrKeyValues, fmt.Errorf("unknown additional attribute: %v", attrKey)
		}
	}

	return attrKeyValues, nil
}
