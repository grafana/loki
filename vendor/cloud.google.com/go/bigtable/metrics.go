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
	"reflect"
	"sync/atomic"
	"time"

	"cloud.google.com/go/bigtable/internal"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/api/option"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
)

const (
	builtInMetricsMeterName = "bigtable.googleapis.com/internal/client/"

	metricsPrefix         = "bigtable/"
	locationMDKey         = "x-goog-ext-425905942-bin"
	serverTimingMDKey     = "server-timing"
	serverTimingValPrefix = "gfet4t7; dur="
	metricMethodPrefix    = "Bigtable."

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
	metricNameOperationLatencies      = "operation_latencies"
	metricNameAttemptLatencies        = "attempt_latencies"
	metricNameServerLatencies         = "server_latencies"
	metricNameAppBlockingLatencies    = "application_latencies"
	metricNameClientBlockingLatencies = "throttling_latencies"
	metricNameFirstRespLatencies      = "first_response_latencies"
	metricNameRetryCount              = "retry_count"
	metricNameDebugTags               = "debug_tags"
	metricNameConnErrCount            = "connectivity_error_count"

	// Metric units
	metricUnitMS    = "ms"
	metricUnitCount = "1"
	maxAttrsLen     = 12 // Monitored resource labels +  Metric labels
)

type contextKey string

const (
	statsContextKey contextKey = "bigtable/clientBlockingLatencyTracker"
)

// These are effectively constant, but for testing purposes they are mutable
var (
	// duration between two metric exports
	defaultSamplePeriod = time.Minute

	disabledMetricsTracerFactory = &builtinMetricsTracerFactory{
		enabled:  false,
		shutdown: func() {},
	}

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
		metricNameFirstRespLatencies: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
			},
			recordedPerAttempt: false,
		},
		metricNameAppBlockingLatencies: {},
		metricNameClientBlockingLatencies: {
			recordedPerAttempt: true,
		},
		metricNameRetryCount: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
			},
			recordedPerAttempt: true,
		},
		metricNameConnErrCount: {
			additionalAttrs: []string{
				metricLabelKeyStatus,
			},
			recordedPerAttempt: true,
		},
	}

	// Generates unique client ID in the format go-<random UUID>@<hostname>
	generateClientUID = func() (string, error) {
		hostname, err := os.Hostname()
		if err != nil {
			return "", err
		}
		return "go-" + uuid.NewString() + "@" + hostname, nil
	}

	endpointOptionType = reflect.TypeOf(option.WithEndpoint(""))

	// GCM exporter should use the same options as Bigtable client
	// createExporterOptions takes Bigtable client options and returns exporter options,
	// filtering out any WithEndpoint option to ensure the metrics exporter uses its default endpoint.
	// Overwritten in tests
	createExporterOptions = func(btOpts ...option.ClientOption) []option.ClientOption {
		filteredOptions := []option.ClientOption{}
		for _, opt := range btOpts {
			if reflect.TypeOf(opt) != endpointOptionType {
				filteredOptions = append(filteredOptions, opt)
			}
		}
		return filteredOptions
	}

	sharedLatencyStatsHandler = &latencyStatsHandler{}
)

type metricInfo struct {
	additionalAttrs    []string
	recordedPerAttempt bool
}

type builtinMetricsTracerFactory struct {
	enabled bool

	clientOpts []option.ClientOption

	// To be called on client close
	shutdown func()

	// attributes that are specific to a client instance and
	// do not change across different function calls on client
	clientAttributes []attribute.KeyValue

	operationLatencies      metric.Float64Histogram
	serverLatencies         metric.Float64Histogram
	attemptLatencies        metric.Float64Histogram
	firstRespLatencies      metric.Float64Histogram
	appBlockingLatencies    metric.Float64Histogram
	clientBlockingLatencies metric.Float64Histogram
	retryCount              metric.Int64Counter
	connErrCount            metric.Int64Counter
	debugTags               metric.Int64Counter
}

// Returns error only if metricsProvider is of unknown type. Rest all errors are swallowed
func newBuiltinMetricsTracerFactory(ctx context.Context, project, instance, appProfile string, metricsProvider MetricsProvider, opts ...option.ClientOption) (*builtinMetricsTracerFactory, error) {
	if metricsProvider != nil {
		switch metricsProvider.(type) {
		case NoopMetricsProvider:
			return disabledMetricsTracerFactory, nil
		default:
			return disabledMetricsTracerFactory, errors.New("bigtable: unknown MetricsProvider type")
		}
	}

	// Metrics are enabled.
	clientUID, err := generateClientUID()
	if err != nil {
		// Swallow the error and disable metrics
		return disabledMetricsTracerFactory, nil
	}

	tracerFactory := &builtinMetricsTracerFactory{
		enabled: true,
		clientAttributes: []attribute.KeyValue{
			attribute.String(monitoredResLabelKeyProject, project),
			attribute.String(monitoredResLabelKeyInstance, instance),
			attribute.String(metricLabelKeyAppProfile, appProfile),
			attribute.String(metricLabelKeyClientUID, clientUID),
			attribute.String(metricLabelKeyClientName, clientName),
		},
		shutdown: func() {},
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
		project:         project,
		instance:        instance,
		appProfile:      appProfile,
		clientName:      clientName,
		clientUID:       clientUID,
		interval:        defaultSamplePeriod,
		customExporter:  nil,
		manualReader:    nil,
		disableExporter: false,
		resourceOpts:    nil,
	})

	// the error from newOtelMetricsContext is silently ignored since metrics are not critical to client creation.
	if err == nil {
		tracerFactory.clientOpts = otelContext.clientOpts
	}
	tracerFactory.shutdown = func() {
		if otelContext != nil {
			otelContext.close()
		}
		meterProvider.Shutdown(ctx)
	}

	// Create meter and instruments
	meter := meterProvider.Meter(builtInMetricsMeterName, metric.WithInstrumentationVersion(internal.Version))
	err = tracerFactory.createInstruments(meter)
	if err != nil {
		// Swallow the error and disable metrics
		return disabledMetricsTracerFactory, nil
	}
	// Swallow the error and disable metrics
	return tracerFactory, nil
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

func (tf *builtinMetricsTracerFactory) newAsyncRefreshErrHandler() func() {
	if !tf.enabled {
		return func() {}
	}

	asyncRefreshMetricAttrs := tf.clientAttributes
	asyncRefreshMetricAttrs = append(asyncRefreshMetricAttrs,
		attribute.String(metricLabelKeyTag, "async_refresh_dry_run"),
		// Table, cluster and zone are unknown at this point
		// Use default values
		attribute.String(monitoredResLabelKeyTable, defaultTable),
		attribute.String(monitoredResLabelKeyCluster, defaultCluster),
		attribute.String(monitoredResLabelKeyZone, defaultZone),
	)
	return func() {
		tf.debugTags.Add(context.Background(), 1,
			metric.WithAttributes(asyncRefreshMetricAttrs...))
	}
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

	// Create first_response_latencies
	tf.firstRespLatencies, err = meter.Float64Histogram(
		metricNameFirstRespLatencies,
		metric.WithDescription("Latency from operation start until the response headers were received. The publishing of the measurement will be delayed until the attempt response has been received."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create application_latencies
	tf.appBlockingLatencies, err = meter.Float64Histogram(
		metricNameAppBlockingLatencies,
		metric.WithDescription("The latency of the client application consuming available response data."),
		metric.WithUnit(metricUnitMS),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)
	if err != nil {
		return err
	}

	// Create client_blocking_latencies
	tf.clientBlockingLatencies, err = meter.Float64Histogram(
		metricNameClientBlockingLatencies,
		metric.WithDescription("The latencies of requests queued on gRPC channels."),
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

	// Create connectivity_error_count
	tf.connErrCount, err = meter.Int64Counter(
		metricNameConnErrCount,
		metric.WithDescription("Number of requests that failed to reach the Google datacenter. (Requests without google response headers"),
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

	instrumentOperationLatencies      metric.Float64Histogram
	instrumentServerLatencies         metric.Float64Histogram
	instrumentAttemptLatencies        metric.Float64Histogram
	instrumentFirstRespLatencies      metric.Float64Histogram
	instrumentAppBlockingLatencies    metric.Float64Histogram
	instrumentClientBlockingLatencies metric.Float64Histogram
	instrumentRetryCount              metric.Int64Counter
	instrumentConnErrCount            metric.Int64Counter
	instrumentDebugTags               metric.Int64Counter

	tableName   string
	method      string
	isStreaming bool

	currOp opTracer
}

// opTracer is used to record metrics for the entire operation, including retries.
// Operation is a logical unit that represents a single method invocation on client.
// The method might require multiple attempts/rpcs and backoff logic to complete
type opTracer struct {
	attemptCount int64

	startTime time.Time

	// Only for ReadRows. Time when the response headers are received in a streaming RPC.
	firstRespTime time.Time

	// gRPC status code of last completed attempt
	status string

	currAttempt attemptTracer

	appBlockingLatency float64
}

func (o *opTracer) setStartTime(t time.Time) {
	o.startTime = t
}

func (o *opTracer) setFirstRespTime(t time.Time) {
	o.firstRespTime = t
}

func (o *opTracer) setStatus(status string) {
	o.status = status
}

func (o *opTracer) incrementAttemptCount() {
	o.attemptCount++
}

func (o *opTracer) incrementAppBlockingLatency(latency float64) {
	o.appBlockingLatency += latency
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

	// Error seen while getting server latency from headers/trailers
	serverLatencyErr error

	// Tracker for client blocking latency
	blockingLatencyTracker *blockingLatencyTracker
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
	if !tf.enabled {
		return builtinMetricsTracer{builtInEnabled: false}
	}
	// Operation has started but not the attempt.
	// So, create only operation tracer and not attempt tracer
	currOpTracer := opTracer{}
	currOpTracer.setStartTime(time.Now())

	return builtinMetricsTracer{
		ctx:            ctx,
		builtInEnabled: tf.enabled,

		currOp:           currOpTracer,
		clientAttributes: tf.clientAttributes,

		instrumentOperationLatencies:      tf.operationLatencies,
		instrumentServerLatencies:         tf.serverLatencies,
		instrumentAttemptLatencies:        tf.attemptLatencies,
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

func (mt *builtinMetricsTracer) setMethod(m string) {
	mt.method = metricMethodPrefix + m
}

// toOtelMetricAttrs:
// - converts metric attributes values captured throughout the operation / attempt
// to OpenTelemetry attributes format,
// - combines these with common client attributes and returns
func (mt *builtinMetricsTracer) toOtelMetricAttrs(metricName string) (attribute.Set, error) {
	attrKeyValues := make([]attribute.KeyValue, 0, maxAttrsLen)
	// Create attribute key value pairs for attributes common to all metricss
	attrKeyValues = append(attrKeyValues,
		attribute.String(metricLabelKeyMethod, mt.method),

		// Add resource labels to otel metric labels.
		// These will be used for creating the monitored resource but exporter
		// will not add them to Google Cloud Monitoring metric labels
		attribute.String(monitoredResLabelKeyTable, mt.tableName),

		// Irrespective of whether metric is attempt specific or operation specific,
		// use last attempt's cluster and zone
		attribute.String(monitoredResLabelKeyCluster, mt.currOp.currAttempt.clusterID),
		attribute.String(monitoredResLabelKeyZone, mt.currOp.currAttempt.zoneID),
	)
	attrKeyValues = append(attrKeyValues, mt.clientAttributes...)

	// Get metric details
	mDetails, found := metricsDetails[metricName]
	if !found {
		return attribute.Set{}, fmt.Errorf("unable to create attributes list for unknown metric: %v", metricName)
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
			return attribute.Set{}, fmt.Errorf("unknown additional attribute: %v", attrKey)
		}
	}

	attrSet := attribute.NewSet(attrKeyValues...)
	return attrSet, nil
}

func (mt *builtinMetricsTracer) recordAttemptStart() {
	if !mt.builtInEnabled {
		return
	}

	// Increment number of attempts
	mt.currOp.incrementAttemptCount()

	mt.currOp.currAttempt = attemptTracer{}

	// record start time
	mt.currOp.currAttempt.setStartTime(time.Now())
}

// recordAttemptCompletion records as many attempt specific metrics as it can
// Ignore errors seen while creating metric attributes since metric can still
// be recorded with rest of the attributes
func (mt *builtinMetricsTracer) recordAttemptCompletion(attemptHeaderMD, attempTrailerMD metadata.MD, err error) {
	if !mt.builtInEnabled {
		return
	}

	// Set attempt status
	statusCode, _ := convertToGrpcStatusErr(err)
	mt.currOp.currAttempt.setStatus(statusCode.String())

	// Get location attributes from metadata and set it in tracer
	// Ignore get location error since the metric can still be recorded with rest of the attributes
	clusterID, zoneID, _ := extractLocation(attemptHeaderMD, attempTrailerMD)
	mt.currOp.currAttempt.setClusterID(clusterID)
	mt.currOp.currAttempt.setZoneID(zoneID)

	// Set server latency in tracer
	serverLatency, serverLatencyErr := extractServerLatency(attemptHeaderMD, attempTrailerMD)
	mt.currOp.currAttempt.setServerLatencyErr(serverLatencyErr)
	mt.currOp.currAttempt.setServerLatency(serverLatency)

	// Calculate elapsed time
	elapsedTime := convertToMs(time.Since(mt.currOp.currAttempt.startTime))

	// Record attempt_latencies
	attemptLatAttrs, _ := mt.toOtelMetricAttrs(metricNameAttemptLatencies)
	mt.instrumentAttemptLatencies.Record(mt.ctx, elapsedTime, metric.WithAttributeSet(attemptLatAttrs))

	// Record client_blocking_latencies
	var clientBlockingLatencyMs float64
	if mt.currOp.currAttempt.blockingLatencyTracker != nil {
		messageSentNanos := mt.currOp.currAttempt.blockingLatencyTracker.getMessageSentNanos()
		clientBlockingLatencyMs = convertToMs(time.Unix(0, int64(messageSentNanos)).Sub(mt.currOp.currAttempt.startTime))
	}
	clientBlockingLatAttrs, _ := mt.toOtelMetricAttrs(metricNameClientBlockingLatencies)
	mt.instrumentClientBlockingLatencies.Record(mt.ctx, clientBlockingLatencyMs, metric.WithAttributeSet(clientBlockingLatAttrs))

	// Record server_latencies
	serverLatAttrs, _ := mt.toOtelMetricAttrs(metricNameServerLatencies)
	if mt.currOp.currAttempt.serverLatencyErr == nil {
		mt.instrumentServerLatencies.Record(mt.ctx, mt.currOp.currAttempt.serverLatency, metric.WithAttributeSet(serverLatAttrs))
	}

	// Record connectivity_error_count
	connErrCountAttrs, _ := mt.toOtelMetricAttrs(metricNameConnErrCount)
	// Determine if connection error should be incremented.
	// A true connectivity error occurs only when we receive NO server-side signals.
	// 1. Server latency (from server-timing header) is a signal, but absent in DirectPath.
	// 2. Location (from x-goog-ext header) is a signal present in both paths.
	// Therefore, we only count an error if BOTH signals are missing.
	isServerLatencyEffectivelyEmpty := mt.currOp.currAttempt.serverLatencyErr != nil || mt.currOp.currAttempt.serverLatency == 0
	isLocationEmpty := mt.currOp.currAttempt.clusterID == defaultCluster
	if isServerLatencyEffectivelyEmpty && isLocationEmpty {
		// This is a connectivity error: the request likely never reached Google's network.
		mt.instrumentConnErrCount.Add(mt.ctx, 1, metric.WithAttributeSet(connErrCountAttrs))
	} else {
		mt.instrumentConnErrCount.Add(mt.ctx, 0, metric.WithAttributeSet(connErrCountAttrs))
	}
}

// recordOperationCompletion records as many operation specific metrics as it can
// Ignores error seen while creating metric attributes since metric can still
// be recorded with rest of the attributes
func (mt *builtinMetricsTracer) recordOperationCompletion() {
	if !mt.builtInEnabled {
		return
	}

	// Calculate elapsed time
	elapsedTimeMs := convertToMs(time.Since(mt.currOp.startTime))

	// Record operation_latencies
	opLatAttrs, _ := mt.toOtelMetricAttrs(metricNameOperationLatencies)
	mt.instrumentOperationLatencies.Record(mt.ctx, elapsedTimeMs, metric.WithAttributeSet(opLatAttrs))

	// Record first_reponse_latencies
	firstRespLatAttrs, _ := mt.toOtelMetricAttrs(metricNameFirstRespLatencies)
	if mt.method == metricMethodPrefix+methodNameReadRows {
		elapsedTimeMs = convertToMs(mt.currOp.firstRespTime.Sub(mt.currOp.startTime))
		mt.instrumentFirstRespLatencies.Record(mt.ctx, elapsedTimeMs, metric.WithAttributeSet(firstRespLatAttrs))
	}

	// Record retry_count
	retryCntAttrs, _ := mt.toOtelMetricAttrs(metricNameRetryCount)
	if mt.currOp.attemptCount > 1 {
		// Only record when retry count is greater than 0 so the retry
		// graph will be less confusing
		mt.instrumentRetryCount.Add(mt.ctx, mt.currOp.attemptCount-1, metric.WithAttributeSet(retryCntAttrs))
	}

	// Record application_latencies
	appBlockingLatAttrs, _ := mt.toOtelMetricAttrs(metricNameAppBlockingLatencies)
	mt.instrumentAppBlockingLatencies.Record(mt.ctx, mt.currOp.appBlockingLatency, metric.WithAttributeSet(appBlockingLatAttrs))
}

func (mt *builtinMetricsTracer) setCurrOpStatus(status string) {
	if !mt.builtInEnabled {
		return
	}

	mt.currOp.setStatus(status)
}

func (mt *builtinMetricsTracer) incrementAppBlockingLatency(latency float64) {
	if !mt.builtInEnabled {
		return
	}

	mt.currOp.incrementAppBlockingLatency(latency)
}

// blockingLatencyTracker is used to calculate the time between stream creation and the first message send.
type blockingLatencyTracker struct {
	endNanos atomic.Int64
}

func (t *blockingLatencyTracker) recordLatency(end time.Time) {
	endN := end.UnixNano()
	// Ensure that only the time of the first OutPayload event is recorded.
	t.endNanos.CompareAndSwap(0, endN)
}

func (t *blockingLatencyTracker) getMessageSentNanos() int64 {
	return t.endNanos.Load()
}

// latencyStatsHandler is a gRPC stats.Handler to measure client blocking latency.
type latencyStatsHandler struct{}

var _ stats.Handler = (*latencyStatsHandler)(nil)

func (h *latencyStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	// The tracker should already be in the context, added by gaxInvokeWithRecorder.
	return ctx
}

func (h *latencyStatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	tracker, ok := ctx.Value(statsContextKey).(*blockingLatencyTracker)
	if !ok {
		return
	}

	if op, ok := s.(*stats.OutPayload); ok {
		tracker.recordLatency(op.SentTime)
	}
}

func (h *latencyStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *latencyStatsHandler) HandleConn(context.Context, stats.ConnStats) {}
