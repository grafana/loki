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

// Package internal (import path bigtable/internal/metrics) owns the
// OpenTelemetry tracer machinery for the bigtable client —
// per-operation Tracer, per-attempt AttemptTracer, the gRPC
// stats.Handler that drives attempt boundaries, and the Cloud
// Monitoring exporter wiring. Split from the bigtable package so the
// internal/session data-plane can stamp per-attempt attributes
// (cluster_id, zone_id, transport labels, client-blocking latency,
// server latency) on session-path calls without an import cycle.
package internal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"

	"cloud.google.com/go/bigtable/internal"
)

// BuiltInMetricsMeterName is the OTel meter name the built-in metrics
// factory registers its instruments under. Cloud Monitoring derives
// the metric-descriptor prefix from this value.
const BuiltInMetricsMeterName = "bigtable.googleapis.com/internal/client/"

const (
	// LocationMDKey is the response-metadata key the server uses to
	// carry the serialized ResponseParams proto with the cluster/zone
	// that served the request.
	LocationMDKey = "x-goog-ext-425905942-bin"
	// ServerTimingMDKey is the response-metadata key the server uses to
	// carry GFE latency (matches the standard Server-Timing HTTP
	// header).
	ServerTimingMDKey     = "server-timing"
	serverTimingValPrefix = "gfet4t7; dur="
	metricMethodPrefix    = "Bigtable."
)

// Metric attribute label keys. project_id / instance / table / cluster
// / zone double as the monitored-resource labels the Cloud Monitoring
// exporter promotes off the metric (see monitoring_exporter.go's
// monitoredResLabelsSet).
const (
	MetricLabelKeyProject            = "project_id"
	MetricLabelKeyInstance           = "instance"
	MetricLabelKeyTable              = "table"
	MetricLabelKeyCluster            = "cluster"
	MetricLabelKeyZone               = "zone"
	MetricLabelKeyAppProfile         = "app_profile"
	MetricLabelKeyMethod             = "method"
	MetricLabelKeyStatus             = "status"
	MetricLabelKeyTag                = "tag"
	MetricLabelKeyStreamingOperation = "streaming"
	MetricLabelKeyClientName         = "client_name"
	MetricLabelKeyClientUID          = "client_uid"
)

// Peer-info-derived attributes recorded only on attempt_latencies2.
// Populated from the bigtable-peer-info sideband metadata via
// ExtractPeerInfo.
const (
	MetricTransportType    = "transport_type"
	MetricTransportRegion  = "transport_region"
	MetricTransportSubZone = "transport_subzone"
	MetricTransportZone    = "transport_zone"
)

// methodNameReadRows is the method label emitted on operation- and
// first-response-latency metrics for ReadRows calls. Duplicated from
// bigtable.methodNameReadRows so the tracer can name-match without
// importing the bigtable package. Only ReadRows currently records
// first_response_latencies; the constant lives here rather than in a
// per-method table because there is no other method that needs
// special-casing in this file.
const methodNameReadRows = "ReadRows"

// OTel instrument names. These are the on-the-wire metric names Cloud
// Monitoring stores; changing these strings would break dashboards.
const (
	MetricNameOperationLatencies      = "operation_latencies"
	MetricNameAttemptLatencies        = "attempt_latencies"
	MetricNameAttemptLatencies2       = "attempt_latencies2"
	MetricNameServerLatencies         = "server_latencies"
	MetricNameAppBlockingLatencies    = "application_latencies"
	MetricNameClientBlockingLatencies = "throttling_latencies"
	MetricNameFirstRespLatencies      = "first_response_latencies"
	MetricNameRetryCount              = "retry_count"
	MetricNameDebugTags               = "debug_tags"
	MetricNameConnErrCount            = "connectivity_error_count"
)

const (
	metricUnitMS    = "ms"
	metricUnitCount = "1"
)

type contextKey string

const (
	statsContextKey         contextKey = "bigtable/clientBlockingLatencyTracker"
	metricsTracerContextKey contextKey = "bigtable/metricsTracer"
)

// NewContext returns a copy of ctx that carries mt so downstream
// callers (retry loop, gRPC stats.Handler) can recover it via
// FromContext.
func NewContext(ctx context.Context, mt *Tracer) context.Context {
	return context.WithValue(ctx, metricsTracerContextKey, mt)
}

// FromContext returns the Tracer stashed on ctx by NewContext. If ctx
// carries none (metrics disabled, or a test call bypassing the retry
// loop) it returns a stub Tracer with BuiltInEnabled=false so callers
// don't need to nil-check.
func FromContext(ctx context.Context) *Tracer {
	if mt, ok := ctx.Value(metricsTracerContextKey).(*Tracer); ok {
		return mt
	}
	return &Tracer{
		BuiltInEnabled: false,
		currOp: OpTracer{
			cookies: make(map[string]string),
		},
	}
}

// These are effectively constant, but for testing purposes they are mutable
var (
	// MetricsErrorPrefix wraps every metrics-subsystem error surfaced to
	// the OTel error handler. Exposed so tests can assert that exporter
	// / handler failures make it into the error stream.
	MetricsErrorPrefix = "bigtable-metrics: "

	clientName = fmt.Sprintf("go-bigtable/%v", internal.Version)

	bucketBounds = []float64{0.0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 8.0, 10.0, 13.0, 16.0, 20.0, 25.0, 30.0, 40.0,
		50.0, 65.0, 80.0, 100.0, 130.0, 160.0, 200.0, 250.0, 300.0, 400.0, 500.0, 650.0,
		800.0, 1000.0, 2000.0, 5000.0, 10000.0, 20000.0, 50000.0, 100000.0, 200000.0,
		400000.0, 800000.0, 1600000.0, 3200000.0}

	// clientBlockingBucketBounds bounds optimized for microsecond-scale
	// latencies (expressed in milliseconds), ranging from 10µs to 10s.
	clientBlockingBucketBounds = []float64{
		0.0, 0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.08, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.8, 1.0,
		2.0, 5.0, 10.0, 20.0, 50.0, 100.0, 500.0, 1000.0, 5000.0, 10000.0,
	}

	// All the built-in metrics have same attributes except 'tag', 'status' and 'streaming'
	// These attributes need to be added to only few of the metrics
	MetricsDetails = map[string]metricInfo{
		MetricNameOperationLatencies: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
				MetricLabelKeyStreamingOperation,
			},
			recordedPerAttempt: false,
		},
		MetricNameAttemptLatencies: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
				MetricLabelKeyStreamingOperation,
			},
			recordedPerAttempt: true,
		},
		MetricNameAttemptLatencies2: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
				MetricLabelKeyStreamingOperation,
				MetricTransportType,
				MetricTransportRegion,
				MetricTransportSubZone,
				MetricTransportZone,
			},
			recordedPerAttempt: true,
		},
		MetricNameServerLatencies: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
				MetricLabelKeyStreamingOperation,
			},
			recordedPerAttempt: true,
		},
		MetricNameFirstRespLatencies: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
			},
			recordedPerAttempt: false,
		},
		MetricNameAppBlockingLatencies: {},
		MetricNameClientBlockingLatencies: {
			recordedPerAttempt: true,
		},
		MetricNameRetryCount: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
			},
			recordedPerAttempt: true,
		},
		MetricNameConnErrCount: {
			additionalAttrs: []string{
				MetricLabelKeyStatus,
			},
			recordedPerAttempt: true,
		},
	}

	SharedStatsHandler = &StatsHandler{}
)

type metricInfo struct {
	additionalAttrs    []string
	recordedPerAttempt bool
}

// Tracer is created one per operation
// It is used to store metric instruments, attribute values
// and other data required to obtain and record them.
//
// mu serializes writes from ingestMetadata (called on the transport
// reader goroutine from InHeader/InTrailer) against reads from
// RecordAttemptCompletion / RecordOperationCompletion (which may run
// on the caller goroutine when csAttempt.finish dispatches End under
// cancel/deadline/GOAWAY). Held only around the field
// reads/writes — not across metric.Record calls — but a per-attempt
// mutex has no realistic contention beyond that rare race window.
type Tracer struct {
	mu             sync.Mutex
	ctx            context.Context
	BuiltInEnabled bool

	// attributes that are specific to a client instance and
	// do not change across different operations on client
	clientAttributes []attribute.KeyValue

	instrumentOperationLatencies      metric.Float64Histogram
	instrumentServerLatencies         metric.Float64Histogram
	instrumentAttemptLatencies        metric.Float64Histogram
	instrumentAttemptLatencies2       metric.Float64Histogram
	instrumentFirstRespLatencies      metric.Float64Histogram
	instrumentAppBlockingLatencies    metric.Float64Histogram
	instrumentClientBlockingLatencies metric.Float64Histogram
	instrumentRetryCount              metric.Int64Counter
	instrumentConnErrCount            metric.Int64Counter
	instrumentDebugTags               metric.Int64Counter

	tableName   string
	method      string
	isStreaming bool

	currOp OpTracer
}

// OpTracer is used to record metrics for the entire operation, including retries.
// Operation is a logical unit that represents a single method invocation on client.
// The method might require multiple attempts/rpcs and backoff logic to complete
type OpTracer struct {
	attemptCount int64

	startTime time.Time

	// Only for ReadRows. Time when the response headers are received in a streaming RPC.
	firstRespTime time.Time

	// gRPC status code of last completed attempt
	status string

	currAttempt AttemptTracer

	appBlockingLatency float64

	// For routing cookie and gRPC attempt number
	cookies map[string]string

	// Last known location details across all attempts
	lastClusterID string
	lastZoneID    string
}

// SetStartTime stamps the operation start time used by
// recordOperationCompletion to compute operation_latencies.
func (o *OpTracer) SetStartTime(t time.Time) {
	o.startTime = t
}

func (o *OpTracer) setFirstRespTime(t time.Time) {
	o.firstRespTime = t
}

func (o *OpTracer) setStatus(status string) {
	o.status = status
}

func (o *OpTracer) incrementAttemptCount() {
	o.attemptCount++
}

// IncrementAppBlockingLatency accumulates application-blocking
// latency (ms) into the operation total that will be recorded onto
// application_latencies at operation completion.
func (o *OpTracer) IncrementAppBlockingLatency(latency float64) {
	o.appBlockingLatency += latency
}

// AttemptTracer is used to record metrics for each individual attempt of the operation.
// Attempt corresponds to an attempt of an RPC.
type AttemptTracer struct {
	startTime time.Time
	clusterID string
	zoneID    string

	// Peer-info-derived attributes (feed attempt_latencies2 only). Populated
	// from the bigtable-peer-info sideband metadata; empty when the server
	// didn't emit the header (older servers, or PeerInfo feature flag off).
	transportType    string
	transportRegion  string
	transportZone    string
	transportSubZone string

	// gRPC status code
	status string

	// Server latency in ms
	serverLatency float64

	// Error seen while getting server latency from headers/trailers
	serverLatencyErr error

	// Tracker for client blocking latency
	blockingLatencyTracker *blockingLatencyTracker

	// Client blocking latency in ms
	clientBlockingLatency float64

	// locationExtracted is set once cluster/zone have been read from a
	// response-metadata frame. Guards against a later InTrailer
	// clobbering values that InHeader already populated.
	locationExtracted bool

	// peerInfoExtracted is set once the bigtable-peer-info sideband
	// metadata has been read. Same header-preferred-trailer-fallback
	// rule as locationExtracted.
	peerInfoExtracted bool
}

// SetStartTime stamps the attempt start time. attempt_latencies and
// attempt_latencies2 are recorded as (now - startTime) at attempt
// completion.
func (a *AttemptTracer) SetStartTime(t time.Time) {
	a.startTime = t
}

// SetClusterID stamps the "cluster" attribute derived from the
// LocationMDKey response metadata.
func (a *AttemptTracer) SetClusterID(clusterID string) {
	a.clusterID = clusterID
}

// SetZoneID stamps the "zone" attribute derived from the LocationMDKey
// response metadata.
func (a *AttemptTracer) SetZoneID(zoneID string) {
	a.zoneID = zoneID
}

func (a *AttemptTracer) setStatus(status string) {
	a.status = status
}

// SetServerLatency stamps the GFE-reported server latency (ms) that
// will be recorded onto server_latencies at attempt completion.
func (a *AttemptTracer) SetServerLatency(latency float64) {
	a.serverLatency = latency
}

func (a *AttemptTracer) setServerLatencyErr(err error) {
	a.serverLatencyErr = err
}

// SetClientBlockingLatency stamps the per-attempt client-blocking
// latency. The session data plane uses this because it computes the
// value from btransport.InvokeResult.SentAt rather than relying on the
// gRPC OutPayload stats event that never fires for vRPC frames.
func (a *AttemptTracer) SetClientBlockingLatency(ms float64) {
	a.clientBlockingLatency = ms
}

// SetTransportType stamps the transport_type label used on
// attempt_latencies2. Session-path callers populate this from the
// serving session's parsed PeerInfo.
func (a *AttemptTracer) SetTransportType(v string) { a.transportType = v }

// SetTransportRegion stamps the transport_region label.
func (a *AttemptTracer) SetTransportRegion(v string) { a.transportRegion = v }

// SetTransportZone stamps the transport_zone label.
func (a *AttemptTracer) SetTransportZone(v string) { a.transportZone = v }

// SetTransportSubZone stamps the transport_subzone label.
func (a *AttemptTracer) SetTransportSubZone(v string) { a.transportSubZone = v }

// StartTime returns when the attempt started — session-path callers
// need it to compute (result.SentAt - startTime) for
// clientBlockingLatency stamping.
func (a *AttemptTracer) StartTime() time.Time { return a.startTime }

// SetMethod stamps the "method" label the tracer emits on every
// metric for this operation. Callers pass the short method name
// (e.g. "ReadRows"); the tracer prepends "Bigtable." for the wire.
func (mt *Tracer) SetMethod(m string) {
	mt.method = metricMethodPrefix + m
}

// toOtelMetricAttrs:
// - converts metric attributes values captured throughout the operation / attempt
// to OpenTelemetry attributes format,
// - combines these with common client attributes and returns
func (mt *Tracer) toOtelMetricAttrs(metricName string) (attribute.Set, error) {
	// Get metric details
	mDetails, found := MetricsDetails[metricName]
	if !found {
		return attribute.Set{}, fmt.Errorf("unable to create attributes list for unknown metric: %v", metricName)
	}

	clusterID := mt.currOp.currAttempt.clusterID
	zoneID := mt.currOp.currAttempt.zoneID
	status := mt.currOp.status

	if mDetails.recordedPerAttempt {
		status = mt.currOp.currAttempt.status
	} else {
		clusterID = FallbackString(clusterID, mt.currOp.lastClusterID)
		zoneID = FallbackString(zoneID, mt.currOp.lastZoneID)
	}
	// Cloud Monitoring rejects bigtable_client_raw writes with an empty
	// zone/cluster ("Unrecognized region or location"). Empty values reach
	// here when an attempt errors before response headers/trailers arrive
	// (e.g. DEADLINE_EXCEEDED on the first attempt), so ExtractLocation
	// never runs and there's no lastClusterID/lastZoneID to fall back on.
	clusterID = FallbackString(clusterID, defaultCluster)
	zoneID = FallbackString(zoneID, defaultZone)

	// 4 fixed attributes below (method / table / cluster / zone) plus the
	// per-client attributes plus this metric's additional attributes.
	attrKeyValues := make([]attribute.KeyValue, 0, 4+len(mt.clientAttributes)+len(mDetails.additionalAttrs))
	// Create attribute key value pairs for attributes common to all metricss
	attrKeyValues = append(attrKeyValues,
		attribute.String(MetricLabelKeyMethod, mt.method),

		// Add resource labels to otel metric labels.
		// These will be used for creating the monitored resource but exporter
		// will not add them to Google Cloud Monitoring metric labels
		attribute.String(MetricLabelKeyTable, mt.tableName),

		attribute.String(MetricLabelKeyCluster, clusterID),
		attribute.String(MetricLabelKeyZone, zoneID),
	)
	attrKeyValues = append(attrKeyValues, mt.clientAttributes...)

	// Add additional attributes to metrics
	for _, attrKey := range mDetails.additionalAttrs {
		switch attrKey {
		case MetricLabelKeyStatus:
			attrKeyValues = append(attrKeyValues, attribute.String(MetricLabelKeyStatus, status))
		case MetricLabelKeyStreamingOperation:
			attrKeyValues = append(attrKeyValues, attribute.Bool(MetricLabelKeyStreamingOperation, mt.isStreaming))
		case MetricTransportType:
			attrKeyValues = append(attrKeyValues, attribute.String(MetricTransportType, mt.currOp.currAttempt.transportType))
		case MetricTransportRegion:
			attrKeyValues = append(attrKeyValues, attribute.String(MetricTransportRegion, mt.currOp.currAttempt.transportRegion))
		case MetricTransportSubZone:
			attrKeyValues = append(attrKeyValues, attribute.String(MetricTransportSubZone, mt.currOp.currAttempt.transportSubZone))
		case MetricTransportZone:
			attrKeyValues = append(attrKeyValues, attribute.String(MetricTransportZone, mt.currOp.currAttempt.transportZone))
		default:
			return attribute.Set{}, fmt.Errorf("unknown additional attribute: %v", attrKey)
		}
	}

	attrSet := attribute.NewSet(attrKeyValues...)
	return attrSet, nil
}

// RecordAttemptStart resets the per-attempt state on the tracer and
// stamps the attempt start time. Called by StatsHandler.TagRPC at the
// beginning of every gRPC attempt.
func (mt *Tracer) RecordAttemptStart() {
	if !mt.BuiltInEnabled {
		return
	}

	// Locks against any lingering ingestMetadata from a prior attempt's
	// late-arriving InTrailer (retry paths can race attempt N+1's
	// TagRPC-dispatched RecordAttemptStart with attempt N's transport
	// reader still draining trailers into the shared Tracer).
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Increment number of attempts
	mt.currOp.incrementAttemptCount()

	mt.currOp.currAttempt = AttemptTracer{}

	// record start time
	mt.currOp.currAttempt.SetStartTime(time.Now())
}

// ingestMetadata parses cluster/zone, peer-info, and server latency out
// of a response-metadata frame and stashes the results as primitive
// fields on the current attempt. Called from HandleRPC on the transport
// reader goroutine — once for InHeader (headerMD, nil) and once for
// InTrailer (nil, trailerMD). RecordAttemptCompletion then reads only
// those primitives, so stats.End (which may dispatch on a different
// goroutine under cancel/deadline/GOAWAY) never touches the raw MD.
//
// extractLocation / extractPeerInfo / extractServerLatency all check
// header first then trailer, so passing one MD at a time and gating on
// the *Extracted booleans preserves the header-preferred-trailer-fallback
// behaviour. Server latency uses the "serverLatency == 0" gate directly
// since it has no sentinel-default value distinct from unset.
func (mt *Tracer) ingestMetadata(headerMD, trailerMD metadata.MD) {
	if !mt.BuiltInEnabled {
		return
	}

	mt.mu.Lock()
	defer mt.mu.Unlock()

	a := &mt.currOp.currAttempt

	if !a.locationExtracted {
		// extractLocation returns (defaultCluster, defaultZone, err) when the
		// LocationMDKey header is missing. Stamp the sentinel anyway —
		// RecordAttemptCompletion uses clusterID == defaultCluster as the
		// "no server-side signals" marker that feeds connectivity_error_count.
		// Only mark locationExtracted when we got a real value, so a later
		// InTrailer carrying real data can still overwrite the sentinel.
		clusterID, zoneID, err := extractLocation(headerMD, trailerMD)
		// Don't overwrite a cluster/zone that the vRPC path has
		// already populated (SessionTable sets these directly from
		// the ClusterInformation payload); only fill in if the
		// attempt's value is missing or the sentinel default.
		// lastClusterID / lastZoneID always track the freshest real
		// value so operation-level metrics get a sensible fallback.
		if clusterID != "" {
			if existing := a.clusterID; existing == "" || existing == defaultCluster {
				a.SetClusterID(clusterID)
			}
			if clusterID != defaultCluster {
				mt.currOp.lastClusterID = clusterID
			}
		}
		if zoneID != "" {
			if existing := a.zoneID; existing == "" || existing == defaultZone {
				a.SetZoneID(zoneID)
			}
			if zoneID != defaultZone {
				mt.currOp.lastZoneID = zoneID
			}
		}
		if err == nil {
			a.locationExtracted = true
		}
	}

	if !a.peerInfoExtracted {
		// Extract transport labels from the bigtable-peer-info sideband
		// metadata (populated by the server when the PeerInfo feature
		// flag is negotiated on). Feeds attempt_latencies2 only; other
		// metrics stay on the classic label set.
		//
		// Latch peerInfoExtracted only when extractPeerInfo returned a
		// parsed *PeerInfo (which implies err == nil). Bare
		// (nil, err) — a base64 or proto-decode failure — and bare
		// (nil, nil) — the key is absent — both leave the latch off so
		// the trailer still gets a chance to populate.
		if peerInfo, _ := extractPeerInfo(headerMD, trailerMD); peerInfo != nil {
			a.transportType = TransportTypeName(peerInfo.GetTransportType())
			a.transportRegion = peerInfo.GetApplicationFrontendRegion()
			a.transportZone = peerInfo.GetApplicationFrontendZone()
			a.transportSubZone = peerInfo.GetApplicationFrontendSubzone()
			a.peerInfoExtracted = true
		}
	}

	if a.serverLatency == 0 {
		if latency, err := extractServerLatency(headerMD, trailerMD); err == nil && latency > 0 {
			a.serverLatency = latency
			a.serverLatencyErr = nil
		} else if err != nil {
			// Remember the first extraction error so RecordAttemptCompletion
			// can decide whether to skip the server_latencies metric; a
			// later successful extraction from the trailer clears it.
			a.serverLatencyErr = err
		}
	}
}

// RecordAttemptCompletionWithMetadata is retained as a thin adapter for
// call sites that already hold both metadata bags (e.g. session/vRPC
// paths on downstream branches). New callers should use ingestMetadata
// + RecordAttemptCompletion directly.
func (mt *Tracer) RecordAttemptCompletionWithMetadata(attemptHeaderMD, attempTrailerMD metadata.MD, err error) {
	if !mt.BuiltInEnabled {
		return
	}
	mt.ingestMetadata(attemptHeaderMD, attempTrailerMD)
	mt.RecordAttemptCompletion(err)
}

// RecordAttemptCompletion records as many attempt specific metrics as it can
// Ignore errors seen while creating metric attributes since metric can still
// be recorded with rest of the attributes.
//
// Reads only primitive fields on the current attempt — cluster/zone/peer
// info/server latency must have been populated by ingestMetadata on
// prior InHeader/InTrailer dispatches. See ingestMetadata for why the
// extraction is done there rather than here.
func (mt *Tracer) RecordAttemptCompletion(err error) {
	if !mt.BuiltInEnabled {
		return
	}

	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Set attempt status
	statusCode, _ := ConvertToGrpcStatusErr(err)
	mt.currOp.currAttempt.setStatus(statusCode.String())

	// Calculate client blocking latency from the blockingLatencyTracker;
	// finalized only at attempt end because it depends on the OutPayload
	// stats event landing before End.
	if mt.currOp.currAttempt.blockingLatencyTracker != nil {
		messageSentNanos := mt.currOp.currAttempt.blockingLatencyTracker.getMessageSentNanos()
		if messageSentNanos > 0 {
			mt.currOp.currAttempt.clientBlockingLatency = ConvertToMs(time.Unix(0, messageSentNanos).Sub(mt.currOp.currAttempt.startTime))
		}
	}

	// Calculate elapsed time
	elapsedTime := ConvertToMs(time.Since(mt.currOp.currAttempt.startTime))

	// Record attempt_latencies
	attemptLatAttrs, _ := mt.toOtelMetricAttrs(MetricNameAttemptLatencies)
	mt.instrumentAttemptLatencies.Record(mt.ctx, elapsedTime, metric.WithAttributeSet(attemptLatAttrs))

	// Record attempt_latencies2 — same value, but broken out by transport
	// labels from the peer-info sideband metadata.
	if mt.instrumentAttemptLatencies2 != nil {
		attemptLat2Attrs, _ := mt.toOtelMetricAttrs(MetricNameAttemptLatencies2)
		mt.instrumentAttemptLatencies2.Record(mt.ctx, elapsedTime, metric.WithAttributeSet(attemptLat2Attrs))
	}

	// Record client_blocking_latencies
	clientBlockingLatAttrs, _ := mt.toOtelMetricAttrs(MetricNameClientBlockingLatencies)
	mt.instrumentClientBlockingLatencies.Record(mt.ctx, mt.currOp.currAttempt.clientBlockingLatency, metric.WithAttributeSet(clientBlockingLatAttrs))

	// Record server_latencies
	serverLatAttrs, _ := mt.toOtelMetricAttrs(MetricNameServerLatencies)
	if mt.currOp.currAttempt.serverLatencyErr == nil {
		mt.instrumentServerLatencies.Record(mt.ctx, mt.currOp.currAttempt.serverLatency, metric.WithAttributeSet(serverLatAttrs))
	}

	// Record connectivity_error_count
	connErrCountAttrs, _ := mt.toOtelMetricAttrs(MetricNameConnErrCount)
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

// RecordOperationCompletion records as many operation specific metrics as it can
// Ignores error seen while creating metric attributes since metric can still
// be recorded with rest of the attributes
func (mt *Tracer) RecordOperationCompletion() {
	if !mt.BuiltInEnabled {
		return
	}

	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Calculate elapsed time
	elapsedTimeMs := ConvertToMs(time.Since(mt.currOp.startTime))

	// Record operation_latencies
	opLatAttrs, _ := mt.toOtelMetricAttrs(MetricNameOperationLatencies)
	mt.instrumentOperationLatencies.Record(mt.ctx, elapsedTimeMs, metric.WithAttributeSet(opLatAttrs))

	// Record first_reponse_latencies
	firstRespLatAttrs, _ := mt.toOtelMetricAttrs(MetricNameFirstRespLatencies)
	if mt.method == metricMethodPrefix+methodNameReadRows {
		elapsedTimeMs = ConvertToMs(mt.currOp.firstRespTime.Sub(mt.currOp.startTime))
		mt.instrumentFirstRespLatencies.Record(mt.ctx, elapsedTimeMs, metric.WithAttributeSet(firstRespLatAttrs))
	}

	// Record retry_count
	retryCntAttrs, _ := mt.toOtelMetricAttrs(MetricNameRetryCount)
	if mt.currOp.attemptCount > 1 {
		// Only record when retry count is greater than 0 so the retry
		// graph will be less confusing
		mt.instrumentRetryCount.Add(mt.ctx, mt.currOp.attemptCount-1, metric.WithAttributeSet(retryCntAttrs))
	}

	// Record application_latencies
	appBlockingLatAttrs, _ := mt.toOtelMetricAttrs(MetricNameAppBlockingLatencies)
	mt.instrumentAppBlockingLatencies.Record(mt.ctx, mt.currOp.appBlockingLatency, metric.WithAttributeSet(appBlockingLatAttrs))
}

// SetCurrOpStatus stamps the operation-level status code that will be
// recorded as the "status" label on operation_latencies /
// first_response_latencies at operation completion.
func (mt *Tracer) SetCurrOpStatus(code codes.Code) {
	if !mt.BuiltInEnabled {
		return
	}

	mt.currOp.setStatus(CanonicalString(code))
}

// SetFirstRespTime stamps the first-response timestamp used by the
// first_response_latencies histogram (ReadRows only). Exposed as a
// method so external callers don't need direct OpTracer field access.
func (mt *Tracer) SetFirstRespTime(t time.Time) {
	if !mt.BuiltInEnabled {
		return
	}
	mt.currOp.setFirstRespTime(t)
}

// Cookies returns the operation-scoped routing-cookie map (populated
// from response headers/trailers by ExtractCookiesFromMD). Callers
// iterate it to append cookies to the next outgoing attempt's metadata.
func (mt *Tracer) Cookies() map[string]string {
	return mt.currOp.cookies
}

// ExtractCookiesFromMD stores any headers in md whose key starts with
// cookiePrefix into the tracer's operation-scoped cookie map. Called by
// the classic path's gaxInvokeWithRecorder after each attempt so
// routing cookies persist across retries.
func (mt *Tracer) ExtractCookiesFromMD(md metadata.MD, cookiePrefix string) {
	for k, v := range md {
		if strings.HasPrefix(k, cookiePrefix) {
			mt.currOp.cookies[k] = v[len(v)-1]
		}
	}
}

// CurrAttempt returns a pointer to the in-progress AttemptTracer so
// external callers (e.g. the session-path data plane) can stamp
// per-attempt attributes — cluster_id, zone_id, transport labels,
// client-blocking latency, server latency. Returns nil when metrics
// are disabled so callers can bail cheaply.
func (mt *Tracer) CurrAttempt() *AttemptTracer {
	if !mt.BuiltInEnabled {
		return nil
	}
	return &mt.currOp.currAttempt
}

// IncrementAppBlockingLatency accumulates application-blocking
// latency (ms) onto the operation tracer. Callers use this to record
// how long the application was blocked returning rows to user code
// between attempts.
func (mt *Tracer) IncrementAppBlockingLatency(latency float64) {
	if !mt.BuiltInEnabled {
		return
	}

	mt.currOp.IncrementAppBlockingLatency(latency)
}

// RecordClientBlockingLatency stamps the per-attempt client-blocking latency
// as the elapsed time since the attempt started. The vRPC path calls this
// when it dispatches a request because there is no gRPC OutPayload stats
// event to drive blockingLatencyTracker — without this stamp, the stats
// handler would never populate clientBlockingLatency for vRPC attempts.
func (mt *Tracer) RecordClientBlockingLatency() {
	if !mt.BuiltInEnabled {
		return
	}
	startTime := mt.currOp.currAttempt.startTime
	if !startTime.IsZero() {
		mt.currOp.currAttempt.clientBlockingLatency = ConvertToMs(time.Since(startTime))
	}
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

// StatsHandler is the gRPC stats.Handler that drives per-attempt metrics
// recording. It is the single source of truth for attempt boundaries: TagRPC
// starts a new attempt, HandleRPC observes the OutPayload/Header/Trailer events
// to feed the blocking-latency tracker, and the End event records attempt
// completion with the final status from gRPC (no io.EOF translation needed
// because stats.End.Error is nil on successful stream close).
//
// A *Tracer is plumbed through the call context by the public
// entry points (ReadRows, Apply, etc.) via NewContext. RPCs that
// don't carry a tracer (or carry a disabled one) are observed only for the
// existing blocking-latency tracker if present, so non-Bigtable RPCs on the
// same channel emit no metrics.
type StatsHandler struct{}

var _ stats.Handler = (*StatsHandler)(nil)

// TagRPC implements grpc/stats.Handler. Called once per attempt when
// the client begins the RPC; drives RecordAttemptStart and installs
// the per-attempt blocking-latency tracker onto ctx.
func (h *StatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	mt := FromContext(ctx)
	if !mt.BuiltInEnabled {
		return ctx
	}

	mt.RecordAttemptStart()

	// Set method name if a caller (e.g. gaxInvokeWithRecorder) hasn't already.
	// strings.LastIndex avoids the slice allocation strings.Split would incur
	// on this per-attempt hot path.
	if mt.method == "" {
		if idx := strings.LastIndex(info.FullMethodName, "/"); idx != -1 {
			mt.SetMethod(info.FullMethodName[idx+1:])
		} else {
			mt.SetMethod(info.FullMethodName)
		}
	}

	blockTracker := &blockingLatencyTracker{}
	mt.currOp.currAttempt.blockingLatencyTracker = blockTracker
	ctx = context.WithValue(ctx, statsContextKey, blockTracker)

	return ctx
}

// HandleRPC implements grpc/stats.Handler. Fires on every RPC-level
// stats event (OutPayload, OutHeader, InHeader, InTrailer, End) and
// funnels attempt boundaries + response metadata into the tracer.
func (h *StatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	if tracker, ok := ctx.Value(statsContextKey).(*blockingLatencyTracker); ok {
		if op, ok := s.(*stats.OutPayload); ok {
			tracker.recordLatency(op.SentTime)
		}
	}

	mt := FromContext(ctx)
	if !mt.BuiltInEnabled {
		return
	}
	switch ev := s.(type) {
	case *stats.InHeader:
		// Runs on the transport reader goroutine, once per stream at
		// initial-HEADERS receive time (skipped when the response is
		// trailers-only). Extract everything we care about now; do not
		// retain a pointer to ev.Header, which grpc still owns and may
		// mutate.
		mt.ingestMetadata(ev.Header, nil)
	case *stats.InTrailer:
		// Same goroutine as InHeader — fires from operateHeaders when a
		// HEADERS frame carrying END_STREAM arrives. Second (and last)
		// chance to pull cluster/zone/peer-info out of the response;
		// gated by locationExtracted/peerInfoExtracted so InHeader wins.
		mt.ingestMetadata(nil, ev.Trailer)
	case *stats.End:
		// May dispatch on a different goroutine from InHeader/InTrailer
		// under cancel/deadline/GOAWAY (csAttempt.finish, stream.go:1251).
		// Reads only primitive fields ingestMetadata already wrote — no
		// MD access here, so End is safe to race with a still-pending
		// InTrailer. Any InTrailer that fires after End is effectively
		// dropped (its writes go to an attempt no one reads again),
		// which matches the pre-race-fix behaviour under the same
		// interleave.
		mt.RecordAttemptCompletion(ev.Error)
	}
}

// TagConn implements grpc/stats.Handler. The tracer records no
// per-connection state, so this is a pass-through.
func (h *StatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn implements grpc/stats.Handler. The tracer records no
// per-connection state, so this is a no-op.
func (h *StatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// FallbackString returns a when non-empty, otherwise b. Used at metric
// recording time to fall back to the tracer's last known
// cluster_id/zone_id when the current attempt didn't carry the
// LocationMDKey metadata.
func FallbackString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
