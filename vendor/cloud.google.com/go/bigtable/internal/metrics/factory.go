/*
Copyright 2026 Google LLC

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

// Factory owns the metrics-subsystem lifecycle: OTel meter provider,
// Cloud Monitoring exporter, instrument construction, and shutdown.
// Runtime telemetry collection (Tracer, StatsHandler, per-attempt
// recording) lives in tracer.go.

package internal

import (
	"context"
	"errors"
	"os"
	"reflect"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/api/option"

	"cloud.google.com/go/bigtable/internal"
)

// fineGrainLatencyBounds matches java-bigtable's
// AGGREGATION_WITH_MILLIS_HISTOGRAM: fine sub-ms + coarse tail. Used
// as the attempt_latencies2 bucket boundaries so sub-ms DirectPath
// samples don't collapse into a single [0,1)ms bucket.
var fineGrainLatencyBounds = []float64{
	// Linear 0 → 3ms by 0.1ms (31 boundaries): fine-grained sub-ms.
	0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9,
	1.0, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9,
	2.0, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 3.0,
	// Coarse 4ms → 80ms.
	4.0, 5.0, 6.0, 8.0, 10.0, 13.0, 16.0, 20.0, 25.0, 30.0, 40.0, 50.0, 65.0, 80.0,
	// Coarse 100ms → 900ms.
	100.0, 130.0, 160.0, 200.0, 250.0, 300.0, 400.0, 500.0, 650.0, 800.0, 900.0,
	// Coarse 1s → 50s.
	1000.0, 2000.0, 3000.0, 4000.0, 5000.0, 6000.0, 10000.0, 20000.0, 50000.0,
	// Long tail: 100s → 5000s (~83 min).
	100000.0, 200000.0, 500000.0, 1000000.0, 2000000.0, 5000000.0,
}

var (
	// DefaultSamplePeriod is the interval between two metric exports.
	// Effectively constant, but exposed as a var so tests can shorten it.
	DefaultSamplePeriod = time.Minute

	disabledMetricsTracerFactory = &Factory{
		Enabled:  false,
		Shutdown: func() {},
	}

	// GenerateClientUID returns a unique client ID in the form
	// "go-<uuid>@<hostname>". If os.Hostname fails, "unknown" is used —
	// the UUID alone is still unique enough to identify this client, and
	// dropping the whole ID would disable metrics entirely for what is a
	// non-fatal environmental hiccup.
	GenerateClientUID = func() (string, error) {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		return "go-" + uuid.NewString() + "@" + hostname, nil
	}

	endpointOptionType = reflect.TypeOf(option.WithEndpoint(""))

	// CreateExporterOptions takes Bigtable client options and returns exporter
	// options, filtering out any WithEndpoint option to ensure the metrics
	// exporter uses its default endpoint. Overridden in tests to redirect
	// the exporter to a fake Cloud Monitoring server.
	CreateExporterOptions = func(btOpts ...option.ClientOption) []option.ClientOption {
		filteredOptions := []option.ClientOption{}
		for _, opt := range btOpts {
			if reflect.TypeOf(opt) != endpointOptionType {
				filteredOptions = append(filteredOptions, opt)
			}
		}
		return filteredOptions
	}
)

// Factory holds the OTel meter provider, exporter shutdown hook, and the
// long-lived instrument handles a Tracer stamps values into.
type Factory struct {
	Enabled bool

	ClientOpts []option.ClientOption

	// To be called on client close
	Shutdown func()

	// attributes that are specific to a client instance and
	// do not change across different function calls on client
	clientAttributes []attribute.KeyValue

	// OtelMeterProvider
	OtelMeterProvider metric.MeterProvider

	operationLatencies      metric.Float64Histogram
	serverLatencies         metric.Float64Histogram
	attemptLatencies        metric.Float64Histogram
	attemptLatencies2       metric.Float64Histogram
	firstRespLatencies      metric.Float64Histogram
	appBlockingLatencies    metric.Float64Histogram
	clientBlockingLatencies metric.Float64Histogram
	retryCount              metric.Int64Counter
	connErrCount            metric.Int64Counter
	debugTags               metric.Int64Counter
}

// NewFactory returns a metrics Tracer factory. A nil metricsProvider or
// a DefaultMetricsProvider{} value enables the built-in Cloud
// Monitoring exporter. A NoopMetricsProvider{} disables it. Any other
// implementation is rejected with an error. All other setup failures
// (client-UID / exporter / instrument creation) are swallowed and the
// disabled factory is returned instead, since metrics are not critical
// to client creation.
func NewFactory(ctx context.Context, project, instance, appProfile string, metricsProvider MetricsProvider, opts ...option.ClientOption) (*Factory, error) {
	switch metricsProvider.(type) {
	case nil, DefaultMetricsProvider:
		// fall through to the enabled path
	case NoopMetricsProvider:
		return disabledMetricsTracerFactory, nil
	default:
		return disabledMetricsTracerFactory, errors.New("bigtable: unknown MetricsProvider type")
	}

	// Metrics are Enabled.
	clientUID, err := GenerateClientUID()
	if err != nil {
		// Swallow the error and disable metrics
		return disabledMetricsTracerFactory, nil
	}

	tracerFactory := &Factory{
		Enabled: true,
		clientAttributes: []attribute.KeyValue{
			attribute.String(MetricLabelKeyProject, project),
			attribute.String(MetricLabelKeyInstance, instance),
			attribute.String(MetricLabelKeyAppProfile, appProfile),
			attribute.String(MetricLabelKeyClientUID, clientUID),
			attribute.String(MetricLabelKeyClientName, clientName),
		},
		Shutdown: func() {},
	}

	// Create default meter provider
	mpOptions, err := builtInMeterProviderOptions(project, opts...)
	if err != nil {
		// Swallow the error and disable metrics
		return disabledMetricsTracerFactory, nil
	}
	meterProvider := sdkmetric.NewMeterProvider(mpOptions...)
	// Enable Otel metrics collection
	otelContext, err := newOtelMetricsContext(ctx, metricsConfig{
		project:    project,
		instance:   instance,
		appProfile: appProfile,
		clientName: clientName,
		clientUID:  clientUID,
		interval:   DefaultSamplePeriod,
	})

	// the error from newOtelMetricsContext is silently ignored since metrics are not critical to client creation.
	if err == nil {
		tracerFactory.ClientOpts = otelContext.ClientOpts
		tracerFactory.OtelMeterProvider = otelContext.OtelMeterProvider
	}
	tracerFactory.Shutdown = func() {
		if otelContext != nil {
			otelContext.close()
		}
		meterProvider.Shutdown(ctx)
	}

	// Create meter and instruments
	meter := meterProvider.Meter(BuiltInMetricsMeterName, metric.WithInstrumentationVersion(internal.Version))
	err = tracerFactory.createInstruments(meter)
	if err != nil {
		// Swallow the error and disable metrics
		return disabledMetricsTracerFactory, nil
	}
	return tracerFactory, nil
}

// NewFactoryForTest constructs an enabled Factory backed by the supplied
// MeterProvider. Test-only: skips the built-in Cloud Monitoring exporter
// setup NewFactory does, so callers can inject a ManualReader-backed
// provider and assert on the emitted data points. Production code must
// use NewFactory.
func NewFactoryForTest(project, instance, appProfile string, mp metric.MeterProvider) (*Factory, error) {
	clientUID, err := GenerateClientUID()
	if err != nil {
		return nil, err
	}
	tf := &Factory{
		Enabled: true,
		clientAttributes: []attribute.KeyValue{
			attribute.String(MetricLabelKeyProject, project),
			attribute.String(MetricLabelKeyInstance, instance),
			attribute.String(MetricLabelKeyAppProfile, appProfile),
			attribute.String(MetricLabelKeyClientUID, clientUID),
			attribute.String(MetricLabelKeyClientName, clientName),
		},
		OtelMeterProvider: mp,
		Shutdown:          func() {},
	}
	meter := mp.Meter(BuiltInMetricsMeterName, metric.WithInstrumentationVersion(internal.Version))
	if err := tf.createInstruments(meter); err != nil {
		return nil, err
	}
	return tf, nil
}

func builtInMeterProviderOptions(project string, opts ...option.ClientOption) ([]sdkmetric.Option, error) {
	allOpts := CreateExporterOptions(opts...)
	defaultExporter, err := newMonitoringExporter(context.Background(), project, allOpts...)
	if err != nil {
		return nil, err
	}

	return []sdkmetric.Option{sdkmetric.WithReader(
		sdkmetric.NewPeriodicReader(
			defaultExporter,
			sdkmetric.WithInterval(DefaultSamplePeriod),
		),
	)}, nil
}

// NewAsyncRefreshErrHandler returns a callback that increments the
// debug_tags counter with tag="async_refresh_dry_run". Returns a no-op
// callback on a disabled factory.
func (tf *Factory) NewAsyncRefreshErrHandler() func() {
	if !tf.Enabled {
		return func() {}
	}

	asyncRefreshMetricAttrs := tf.clientAttributes
	asyncRefreshMetricAttrs = append(asyncRefreshMetricAttrs,
		attribute.String(MetricLabelKeyTag, "async_refresh_dry_run"),
		// Table, cluster and zone are unknown at this point
		// Use default values
		attribute.String(MetricLabelKeyTable, defaultTable),
		attribute.String(MetricLabelKeyCluster, defaultCluster),
		attribute.String(MetricLabelKeyZone, defaultZone),
	)
	return func() {
		tf.debugTags.Add(context.Background(), 1,
			metric.WithAttributes(asyncRefreshMetricAttrs...))
	}
}

func (tf *Factory) createInstruments(meter metric.Meter) error {
	var err error

	// Create operation_latencies
	tf.operationLatencies, err = meter.Float64Histogram(
		MetricNameOperationLatencies,
		metric.WithDescription("Total time until final operation success or failure, including retries and backoff."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create attempt_latencies
	tf.attemptLatencies, err = meter.Float64Histogram(
		MetricNameAttemptLatencies,
		metric.WithDescription("Client observed latency per RPC attempt."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create attempt_latencies2 — same latency value as attempt_latencies,
	// broken out with transport_type/region/zone/subzone attributes sourced
	// from the bigtable-peer-info sideband metadata. Uses the
	// java-parity fineGrainLatencyBounds so sub-ms DirectPath samples
	// don't collapse into a single [0,1)ms bucket.
	tf.attemptLatencies2, err = meter.Float64Histogram(
		MetricNameAttemptLatencies2,
		metric.WithDescription("Client observed latency per RPC attempt, labeled by transport type and AFE location."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(fineGrainLatencyBounds...),
	)
	if err != nil {
		return err
	}

	// Create server_latencies
	tf.serverLatencies, err = meter.Float64Histogram(
		MetricNameServerLatencies,
		metric.WithDescription("The latency measured from the moment that the RPC entered the Google data center until the RPC was completed."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create first_response_latencies
	tf.firstRespLatencies, err = meter.Float64Histogram(
		MetricNameFirstRespLatencies,
		metric.WithDescription("Latency from operation start until the response headers were received. The publishing of the measurement will be delayed until the attempt response has been received."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create application_latencies
	tf.appBlockingLatencies, err = meter.Float64Histogram(
		MetricNameAppBlockingLatencies,
		metric.WithDescription("The latency of the client application consuming available response data."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create client_blocking_latencies
	tf.clientBlockingLatencies, err = meter.Float64Histogram(
		MetricNameClientBlockingLatencies,
		metric.WithDescription("The latencies of requests queued on gRPC channels."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(clientBlockingBucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create retry_count
	tf.retryCount, err = meter.Int64Counter(
		MetricNameRetryCount,
		metric.WithDescription("The number of additional RPCs sent after the initial attempt."),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return err
	}

	// Create connectivity_error_count
	tf.connErrCount, err = meter.Int64Counter(
		MetricNameConnErrCount,
		metric.WithDescription("Number of requests that failed to reach the Google datacenter. (Requests without google response headers"),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return err
	}

	// Create debug_tags
	tf.debugTags, err = meter.Int64Counter(
		MetricNameDebugTags,
		metric.WithDescription("A counter of internal client events used for debugging."),
		metric.WithUnit(metricUnitCount),
	)
	return err
}

// CreateTracer returns a per-operation Tracer wired to this factory's
// instruments. Callers stash it on the operation context via
// NewContext so the shared StatsHandler can retrieve it on each
// attempt. Returns *Tracer (not Tracer) because Tracer holds a
// sync.Mutex that guards concurrent HandleRPC dispatches from the
// transport reader and caller goroutines.
func (tf *Factory) CreateTracer(ctx context.Context, tableName string, isStreaming bool) *Tracer {
	// Operation has started but not the attempt.
	// So, create only operation tracer and not attempt tracer
	currOpTracer := OpTracer{
		cookies: make(map[string]string),
	}
	currOpTracer.SetStartTime(time.Now())

	if !tf.Enabled {
		return &Tracer{
			BuiltInEnabled: false,
			currOp:         currOpTracer,
		}
	}

	return &Tracer{
		ctx:            ctx,
		BuiltInEnabled: tf.Enabled,

		currOp:           currOpTracer,
		clientAttributes: tf.clientAttributes,

		instrumentOperationLatencies:      tf.operationLatencies,
		instrumentServerLatencies:         tf.serverLatencies,
		instrumentAttemptLatencies:        tf.attemptLatencies,
		instrumentAttemptLatencies2:       tf.attemptLatencies2,
		instrumentFirstRespLatencies:      tf.firstRespLatencies,
		instrumentAppBlockingLatencies:    tf.appBlockingLatencies,
		instrumentClientBlockingLatencies: tf.clientBlockingLatencies,
		instrumentRetryCount:              tf.retryCount,
		instrumentConnErrCount:            tf.connErrCount,
		instrumentDebugTags:               tf.debugTags,

		tableName:   tableName,
		isStreaming: isStreaming,
	}
}
