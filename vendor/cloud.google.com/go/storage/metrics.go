// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/storage/internal"
	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	customMetricPrefix = "custom.googleapis.com/"
)

// clientMetrics contains the OpenTelemetry metric instruments to record client-side metrics.
type clientMetrics struct {
	provider                  *sdkmetric.MeterProvider
	rpcClientCallDuration     metric.Float64Histogram
	httpClientRequestDuration metric.Float64Histogram
	duration                  metric.Float64Histogram
	operations                metric.Int64Counter
	attempts                  metric.Int64Counter
	requestBodySize           metric.Int64Histogram
	responseBodySize          metric.Int64Histogram
	ttfb                      metric.Float64Histogram
	errors                    metric.Int64Counter
}

func formatMetricWithPrefix(m metricdata.Metrics, prefix string) string {
	return prefix + strings.ReplaceAll(string(m.Name), ".", "/")
}

// isOtelMetricsEnabled checks if Otel metrics are enabled.
// The environment variable GCP_STORAGE_GO_ENABLE_OTEL_METRICS takes precedence.
func isOtelMetricsEnabled(config *storageConfig) bool {
	if config.disableClientMetrics {
		return false
	}
	if valStr, present := os.LookupEnv("GCP_STORAGE_GO_ENABLE_OTEL_METRICS"); present {
		v, err := strconv.ParseBool(valStr)
		if err == nil {
			return v
		}
	}
	return config.enableOtelMetrics
}

// newMetricsGCMExporter creates a Google Cloud Monitoring exporter.
func newMetricsGCMExporter(ctx context.Context, projectID string) (sdkmetric.Exporter, error) {
	exporter, err := mexporter.New(
		mexporter.WithProjectID(projectID),
		mexporter.WithMetricDescriptorTypeFormatter(func(m metricdata.Metrics) string {
			return formatMetricWithPrefix(m, customMetricPrefix)
		}),
		mexporter.WithCreateServiceTimeSeries(),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: creating GCM exporter: %w", err)
	}
	return exporter, nil
}

// initMetrics initializes clientMetrics with a meter provider and registered exporter.
func initMetrics(ctx context.Context, projectID string, config *storageConfig) (*clientMetrics, func(), error) {
	var provider *sdkmetric.MeterProvider
	var ownProvider bool

	if config.meterProvider != nil {
		provider = config.meterProvider
	} else {
		var exporter sdkmetric.Exporter
		var err error
		if config.metricExporter != nil {
			exporter = *config.metricExporter
		} else {
			exporter, err = newMetricsGCMExporter(ctx, projectID)
			if err != nil {
				return nil, nil, err
			}
		}

		interval := time.Minute
		if config.metricInterval > 0 {
			interval = config.metricInterval
		}

		reader := sdkmetric.NewPeriodicReader(&exporterLogSuppressor{Exporter: exporter}, sdkmetric.WithInterval(interval))

		// Static common attributes are defined as Resource Attributes.
		res, err := resource.New(ctx,
			resource.WithAttributes(
				attribute.String("gcp.client.version", internal.Version),
				attribute.String("gcp.client.service", "storage"),
				attribute.String("gcp.client.repo", "googleapis/google-cloud-go"),
				attribute.String("gcp.client.artifact", "cloud.google.com/go/storage"),
			),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("storage: creating metrics resource: %w", err)
		}

		provider = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
			sdkmetric.WithResource(res),
			sdkmetric.WithView(
				sdkmetric.NewView(
					sdkmetric.Instrument{Name: "rpc.client.call.duration", Kind: sdkmetric.InstrumentKindHistogram},
					sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: latencyHistogramBoundaries()}},
				),
				sdkmetric.NewView(
					sdkmetric.Instrument{Name: "http.client.request.duration", Kind: sdkmetric.InstrumentKindHistogram},
					sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: latencyHistogramBoundaries()}},
				),
				sdkmetric.NewView(
					sdkmetric.Instrument{Name: "gcp.client.request.duration", Kind: sdkmetric.InstrumentKindHistogram},
					sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: latencyHistogramBoundaries()}},
				),
				sdkmetric.NewView(
					sdkmetric.Instrument{Name: "gcp.storage.client.operation.ttfb", Kind: sdkmetric.InstrumentKindHistogram},
					sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: latencyHistogramBoundaries()}},
				),
				sdkmetric.NewView(
					sdkmetric.Instrument{Name: "gcp.storage.client.request.body.size", Kind: sdkmetric.InstrumentKindHistogram},
					sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: sizeHistogramBoundaries()}},
				),
				sdkmetric.NewView(
					sdkmetric.Instrument{Name: "gcp.storage.client.response.body.size", Kind: sdkmetric.InstrumentKindHistogram},
					sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: sizeHistogramBoundaries()}},
				),
			),
		)
		ownProvider = true
	}

	meter := provider.Meter("cloud.google.com/go/storage")

	rpcDuration, err := meter.Float64Histogram(
		"rpc.client.call.duration",
		metric.WithDescription("Duration of one gRPC request. Retries not included (Otel)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, nil, err
	}

	httpDuration, err := meter.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("Duration of one HTTP client request. Retried not included (Otel)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, nil, err
	}

	duration, err := meter.Float64Histogram(
		"gcp.client.request.duration",
		metric.WithDescription("Latency of a client operation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, nil, err
	}

	operations, err := meter.Int64Counter(
		"gcp.storage.client.operations",
		metric.WithDescription("Number of GCS client operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, nil, err
	}

	attempts, err := meter.Int64Counter(
		"gcp.storage.client.attempts",
		metric.WithDescription("Number of GCS client attempts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, nil, err
	}

	requestBodySize, err := meter.Int64Histogram(
		"gcp.storage.client.request.body.size",
		metric.WithDescription("Size of GCS client request body"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, nil, err
	}

	responseBodySize, err := meter.Int64Histogram(
		"gcp.storage.client.response.body.size",
		metric.WithDescription("Size of GCS client response body"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, nil, err
	}

	ttfb, err := meter.Float64Histogram(
		"gcp.storage.client.operation.ttfb",
		metric.WithDescription("Time to first byte of GCS client operations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, nil, err
	}

	errors, err := meter.Int64Counter(
		"gcp.storage.client.errors",
		metric.WithDescription("Number of GCS client errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, nil, err
	}

	cm := &clientMetrics{
		provider:                  provider,
		rpcClientCallDuration:     rpcDuration,
		httpClientRequestDuration: httpDuration,
		duration:                  duration,
		operations:                operations,
		attempts:                  attempts,
		requestBodySize:           requestBodySize,
		responseBodySize:          responseBodySize,
		ttfb:                      ttfb,
		errors:                    errors,
	}

	var cleanup func()
	if ownProvider {
		cleanup = func() {
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			provider.Shutdown(shutdownCtx)
		}
	}

	return cm, cleanup, nil
}

// grpcCodeToString maps a gRPC status code to its screaming-snake-case protocol name.
func grpcCodeToString(code codes.Code) string {
	switch code {
	case codes.OK:
		return "OK"
	case codes.Canceled:
		return "CANCELLED"
	case codes.Unknown:
		return "UNKNOWN"
	case codes.InvalidArgument:
		return "INVALID_ARGUMENT"
	case codes.DeadlineExceeded:
		return "DEADLINE_EXCEEDED"
	case codes.NotFound:
		return "NOT_FOUND"
	case codes.AlreadyExists:
		return "ALREADY_EXISTS"
	case codes.PermissionDenied:
		return "PERMISSION_DENIED"
	case codes.ResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case codes.FailedPrecondition:
		return "FAILED_PRECONDITION"
	case codes.Aborted:
		return "ABORTED"
	case codes.OutOfRange:
		return "OUT_OF_RANGE"
	case codes.Unimplemented:
		return "UNIMPLEMENTED"
	case codes.Internal:
		return "INTERNAL"
	case codes.Unavailable:
		return "UNAVAILABLE"
	case codes.DataLoss:
		return "DATA_LOSS"
	case codes.Unauthenticated:
		return "UNAUTHENTICATED"
	default:
		return "UNKNOWN"
	}
}

// computeErrorType maps the request result to the standard error.type values.
func computeErrorType(err error, isHTTP bool, statusCode int64) string {
	if err == nil {
		if isHTTP && statusCode >= 400 {
			return mapHTTPStatusCode(int(statusCode))
		}
		return "OK"
	}

	if err == io.EOF {
		return "OK"
	}

	errStr := strings.ToLower(err.Error())

	if err == context.Canceled || strings.Contains(errStr, "context canceled") {
		return "CANCELLED"
	}

	if err == context.DeadlineExceeded || strings.Contains(errStr, "deadline exceeded") || strings.Contains(errStr, "timeout") {
		return "TIMEOUT"
	}

	if strings.Contains(errStr, "checksum") || strings.Contains(errStr, "mismatch") {
		return "CHECKSUM_MISMATCH"
	}

	if strings.Contains(errStr, "auth") || strings.Contains(errStr, "credentials") || strings.Contains(errStr, "token") || strings.Contains(errStr, "key") {
		return "AUTHENTICATION_ERROR"
	}

	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "dial tcp") || strings.Contains(errStr, "no such host") || strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "connection reset") || strings.Contains(errStr, "eof") {
		return "CONNECTIVITY"
	}

	if !isHTTP {
		if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
			return grpcCodeToString(st.Code())
		}
	}

	if isHTTP {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) {
			return mapHTTPStatusCode(apiErr.Code)
		}
		if statusCode >= 400 {
			return mapHTTPStatusCode(int(statusCode))
		}
	}

	return "UNKNOWN"
}

// mapHTTPStatusCode converts an HTTP status code to a canonical API error string.
// If there is no direct mapping, it returns the numeric string.
func mapHTTPStatusCode(code int) string {
	switch code {
	case 400:
		return "INVALID_ARGUMENT"
	case 401:
		return "UNAUTHENTICATED"
	case 403:
		return "PERMISSION_DENIED"
	case 404:
		return "NOT_FOUND"
	case 409:
		return "ABORTED"
	case 416:
		return "OUT_OF_RANGE"
	case 429:
		return "RESOURCE_EXHAUSTED"
	case 499:
		return "CANCELLED"
	case 500:
		return "INTERNAL"
	case 501:
		return "UNIMPLEMENTED"
	case 503:
		return "UNAVAILABLE"
	case 504:
		return "DEADLINE_EXCEEDED"
	default:
		return strconv.Itoa(code)
	}
}

func (cm *clientMetrics) recordRPC(ctx context.Context, method, target string, duration float64, err error) {
	statusCode := int64(codes.OK)
	if err != nil && err != io.EOF {
		statusCode = int64(status.Code(err))
	}

	service := "google.storage.v2.Storage"
	methodName := method
	if strings.HasPrefix(method, "/") {
		parts := strings.Split(strings.TrimPrefix(method, "/"), "/")
		if len(parts) >= 2 {
			service = parts[0]
			methodName = parts[1]
		}
	}

	errorType := computeErrorType(err, false, statusCode)

	attrs := []attribute.KeyValue{
		attribute.String("rpc.system.name", "grpc"),
		attribute.String("rpc.service", service),
		attribute.String("rpc.method", methodName),
		attribute.Int64("rpc.grpc.status_code", statusCode),
		attribute.Int64("rpc.response.status_code", statusCode),
		attribute.String("server.address", stripPort(target)),
		attribute.String("error.type", errorType),
	}

	cm.rpcClientCallDuration.Record(ctx, duration, metric.WithAttributes(attrs...))

	// Record standard attempt metric: gcp.storage.client.attempts.
	state := metricsStateFromContext(ctx)
	logicalMethod := methodName
	if state != nil {
		logicalMethod = state.method
	}
	attemptAttrs := []attribute.KeyValue{
		attribute.String("rpc.method", logicalMethod),
		attribute.Int64("rpc.grpc.status_code", statusCode),
		attribute.String("error.type", errorType),
	}
	cm.attempts.Add(ctx, 1, metric.WithAttributes(attemptAttrs...))

	// Record standard error metric: gcp.storage.client.errors.
	if err != nil && err != io.EOF {
		errorAttrs := []attribute.KeyValue{
			attribute.String("rpc.method", logicalMethod),
			attribute.String("error.type", errorType),
			attribute.String("gcp.errors.domain", "storage.googleapis.com"),
		}
		cm.errors.Add(ctx, 1, metric.WithAttributes(errorAttrs...))
	}

	// For unary calls, record TTFB equal to the total attempt latency.
	isStreaming := methodName == "ReadObject" || methodName == "WriteObject" || methodName == "BidiReadObject" || methodName == "BidiWriteObject"
	if !isStreaming {
		cm.ttfb.Record(ctx, duration, metric.WithAttributes(attribute.String("rpc.method", logicalMethod)))
	}
}

func (cm *clientMetrics) recordHTTP(ctx context.Context, req *http.Request, resp *http.Response, duration float64, err error) {
	statusCode := int64(0)
	if resp != nil {
		statusCode = int64(resp.StatusCode)
	}

	urlTemplate := computeURLTemplate(req.URL.Path, req.URL.Host)
	errorType := computeErrorType(err, true, statusCode)

	attrs := []attribute.KeyValue{
		attribute.String("rpc.system.name", "http"),
		attribute.String("http.request.method", req.Method),
		attribute.String("url.template", urlTemplate),
		attribute.Int64("http.response.status_code", statusCode),
		attribute.Int64("rpc.response.status_code", statusCode),
		attribute.String("server.address", stripPort(req.URL.Host)),
		attribute.String("error.type", errorType),
	}

	cm.httpClientRequestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
}

// computeURLTemplate extracts a parameterized template path for a given GCS HTTP request URL path.
func computeURLTemplate(path, host string) string {
	// Check for XML host-style: {bucket}.storage.googleapis.com.
	if strings.HasSuffix(host, ".storage.googleapis.com") && host != "storage.googleapis.com" {
		if path == "/" || path == "" {
			return "/"
		}
		return "/{object}"
	}

	// Check for XML path-style or JSON API.
	if !strings.HasPrefix(path, "/storage/") && !strings.HasPrefix(path, "/upload/") && !strings.HasPrefix(path, "/batch") {
		p := strings.TrimPrefix(path, "/")
		parts := strings.SplitN(p, "/", 2)
		if len(parts) == 1 {
			if parts[0] == "" {
				return "/"
			}
			return "/{bucket}"
		}
		return "/{bucket}/{object}"
	}

	// JSON API: /storage/v1/b/bucket-name/o/object-name etc.
	bIdx := strings.Index(path, "/b/")
	if bIdx == -1 {
		return path
	}
	prefix := path[:bIdx+3]
	rest := path[bIdx+3:]

	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 1 {
		return prefix + "{bucket}"
	}

	oRest := parts[1]
	if oRest == "o" {
		return prefix + "{bucket}/o"
	}
	if strings.HasPrefix(oRest, "o/") {
		return prefix + "{bucket}/o/{object}"
	}

	return prefix + "{bucket}/" + oRest
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// metricsRoundTripper is an http.RoundTripper that wraps an underlying transport.
type metricsRoundTripper struct {
	base    http.RoundTripper
	metrics *clientMetrics
}

func (rt *metricsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	startTime := time.Now()
	resp, err := rt.base.RoundTrip(req)

	state := metricsStateFromContext(req.Context())
	var logicalMethod string
	if state != nil {
		logicalMethod = state.method
	} else {
		logicalMethod = "Unknown"
	}

	statusCode := int64(0)
	if resp != nil {
		statusCode = int64(resp.StatusCode)
	}
	errorType := computeErrorType(err, true, statusCode)

	if rt.metrics != nil {
		// Record attempt.
		attemptAttrs := []attribute.KeyValue{
			attribute.String("rpc.method", logicalMethod),
			attribute.Int64("http.response.status_code", statusCode),
			attribute.String("error.type", errorType),
		}
		rt.metrics.attempts.Add(req.Context(), 1, metric.WithAttributes(attemptAttrs...))

		// Record error if failed.
		if err != nil || (resp != nil && resp.StatusCode >= 400) {
			errorAttrs := []attribute.KeyValue{
				attribute.String("rpc.method", logicalMethod),
				attribute.String("error.type", errorType),
				attribute.String("gcp.errors.domain", "storage.googleapis.com"),
			}
			rt.metrics.errors.Add(req.Context(), 1, metric.WithAttributes(errorAttrs...))
		}

		// Record TTFB.
		isDownload := req.Method == "GET" && req.URL.Query().Get("alt") == "media"
		isResumableInit := req.Method == "POST" && strings.Contains(req.URL.Path, "/upload/") && req.URL.Query().Get("uploadType") == "resumable"
		if !isDownload || isResumableInit {
			duration := time.Since(startTime).Seconds()
			rt.metrics.ttfb.Record(req.Context(), duration, metric.WithAttributes(attribute.String("rpc.method", logicalMethod)))
		}
	}

	if err != nil {
		if rt.metrics != nil {
			duration := time.Since(startTime).Seconds()
			rt.metrics.recordHTTP(req.Context(), req, nil, duration, err)
		}
		return nil, err
	}

	if resp.Body != nil {
		resp.Body = &wrappedResponseBody{
			ReadCloser: resp.Body,
			startTime:  startTime,
			req:        req,
			resp:       resp,
			metrics:    rt.metrics,
			isDownload: req.Method == "GET" && req.URL.Query().Get("alt") == "media",
		}
	} else {
		if rt.metrics != nil {
			duration := time.Since(startTime).Seconds()
			rt.metrics.recordHTTP(req.Context(), req, resp, duration, nil)
		}
	}
	return resp, nil
}

type wrappedResponseBody struct {
	io.ReadCloser
	startTime  time.Time
	req        *http.Request
	resp       *http.Response
	metrics    *clientMetrics
	recorded   atomic.Bool
	isDownload bool
	firstRead  atomic.Bool
}

func (w *wrappedResponseBody) Read(p []byte) (n int, err error) {
	if w.isDownload && w.metrics != nil && w.firstRead.CompareAndSwap(false, true) {
		duration := time.Since(w.startTime).Seconds()
		state := metricsStateFromContext(w.req.Context())
		logicalMethod := "ReadObject"
		if state != nil {
			logicalMethod = state.method
		}
		w.metrics.ttfb.Record(w.req.Context(), duration, metric.WithAttributes(attribute.String("rpc.method", logicalMethod)))
	}
	n, err = w.ReadCloser.Read(p)
	if err != nil {
		w.record(err)
	}
	return n, err
}

func (w *wrappedResponseBody) Close() error {
	err := w.ReadCloser.Close()
	w.record(err)
	return err
}

func (w *wrappedResponseBody) record(err error) {
	if w.recorded.CompareAndSwap(false, true) {
		duration := time.Since(w.startTime).Seconds()
		w.metrics.recordHTTP(w.req.Context(), w.req, w.resp, duration, err)
	}
}

// metricsInterceptors returns gRPC client interceptors.
func metricsInterceptors(cm *clientMetrics) (grpc.UnaryClientInterceptor, grpc.StreamClientInterceptor) {
	unary := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		startTime := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		duration := time.Since(startTime).Seconds()
		target := ""
		if cc != nil {
			target = cc.Target()
		}
		cm.recordRPC(ctx, method, target, duration, err)
		return err
	}

	stream := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		startTime := time.Now()
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		target := ""
		if cc != nil {
			target = cc.Target()
		}
		if err != nil {
			duration := time.Since(startTime).Seconds()
			cm.recordRPC(ctx, method, target, duration, err)
			return nil, err
		}

		return &wrappedClientStream{
			ClientStream:  clientStream,
			startTime:     startTime,
			method:        method,
			target:        target,
			metrics:       cm,
			ctx:           ctx,
			serverStreams: desc.ServerStreams,
			clientStreams: desc.ClientStreams,
		}, nil
	}

	return unary, stream
}

type wrappedClientStream struct {
	grpc.ClientStream
	startTime     time.Time
	method        string
	target        string
	metrics       *clientMetrics
	ctx           context.Context
	recorded      atomic.Bool
	serverStreams bool
	clientStreams bool
	recordedTTFB  atomic.Bool
}

func (w *wrappedClientStream) RecvMsg(m interface{}) error {
	err := w.ClientStream.RecvMsg(m)
	if err == nil {
		w.recordTTFB(m)
	}
	// For client-streaming streams (like WriteObject), the single successful RecvMsg call
	// returns the response and nil error, which marks the completion of the stream.
	isClientStreaming := !w.serverStreams && w.clientStreams
	if err != nil || isClientStreaming {
		w.record(err)
	}
	return err
}

func (w *wrappedClientStream) SendMsg(m interface{}) error {
	err := w.ClientStream.SendMsg(m)
	if err != nil {
		w.record(err)
	}
	return err
}

func (w *wrappedClientStream) record(err error) {
	if w.recorded.CompareAndSwap(false, true) {
		duration := time.Since(w.startTime).Seconds()
		w.metrics.recordRPC(w.ctx, w.method, w.target, duration, err)
	}
}

func (w *wrappedClientStream) recordTTFB(m interface{}) {
	if w.recordedTTFB.Load() {
		return
	}
	methodName := w.method
	if strings.HasPrefix(methodName, "/") {
		parts := strings.Split(strings.TrimPrefix(methodName, "/"), "/")
		if len(parts) >= 2 {
			methodName = parts[1]
		}
	}

	// The first response from the server, whether it contains metadata,
	// persisted size, or actual content, indicates TTFB.
	if w.recordedTTFB.CompareAndSwap(false, true) {
		duration := time.Since(w.startTime).Seconds()
		state := metricsStateFromContext(w.ctx)
		logicalMethod := methodName
		if state != nil {
			logicalMethod = state.method
		}
		w.metrics.ttfb.Record(w.ctx, duration, metric.WithAttributes(attribute.String("rpc.method", logicalMethod)))
	}
}

type metricsKey struct{}

type metricsState struct {
	method       string
	startTime    time.Time
	metrics      *clientMetrics
	isHTTP       bool
	ttfbRecorded atomic.Bool
	ttfbStart    time.Time
	record       func(error)
}

func contextWithMetricsState(ctx context.Context, state *metricsState) context.Context {
	return context.WithValue(ctx, metricsKey{}, state)
}

func metricsStateFromContext(ctx context.Context) *metricsState {
	if ctx == nil {
		return nil
	}
	if state, ok := ctx.Value(metricsKey{}).(*metricsState); ok {
		return state
	}
	return nil
}

func contextWithoutMetrics(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, metricsKey{}, (*metricsState)(nil))
}

func (cm *clientMetrics) startOperation(ctx context.Context, method string, isHTTP bool) (context.Context, func(error)) {
	if cm == nil {
		return ctx, func(error) {}
	}
	state := &metricsState{
		method:    method,
		startTime: time.Now(),
		metrics:   cm,
		isHTTP:    isHTTP,
	}
	state.ttfbStart = state.startTime

	var recordOnce sync.Once
	record := func(err error) {
		recordOnce.Do(func() {
			duration := time.Since(state.startTime).Seconds()
			statusStr := "OK"
			if err != nil && err != io.EOF {
				statusStr = "Error"
			}
			errorType := computeErrorType(err, isHTTP, 0)

			attrs := []attribute.KeyValue{
				attribute.String("rpc.method", method),
				attribute.String("status", statusStr),
				attribute.String("error.type", errorType),
			}
			opts := metric.WithAttributes(attrs...)
			cm.duration.Record(ctx, duration, opts)
			cm.operations.Add(ctx, 1, opts)
		})
	}
	state.record = record

	ctx = contextWithMetricsState(ctx, state)
	return ctx, record
}

// startMetricsOp starts a client operation if OpenTelemetry metrics are enabled in ctx.
// It returns the updated context containing metrics state and a recording closure.
func startMetricsOp(ctx context.Context, method string, isHTTP bool) (context.Context, func(error)) {
	if state := metricsStateFromContext(ctx); state != nil && state.metrics != nil {
		return state.metrics.startOperation(ctx, method, isHTTP)
	}
	return ctx, func(error) {}
}

// initClientMetrics initializes OpenTelemetry client metrics if enabled in config.
// It returns the metrics instance and its cleanup function, or nil if disabled or upon error.
func initClientMetrics(ctx context.Context, project string, config *storageConfig) (*clientMetrics, func()) {
	if !isOtelMetricsEnabled(config) {
		return nil, nil
	}
	cm, cleanup, err := initMetrics(ctx, project, config)
	if err != nil {
		log.Printf("Failed to enable metrics: %v", err)
		return nil, nil
	}
	return cm, cleanup
}

// metricsStorageClient wraps a storageClient and records client-level metrics.
type metricsStorageClient struct {
	storageClient
	metrics *clientMetrics
	isHTTP  bool
}

func (mc *metricsStorageClient) GetServiceAccount(ctx context.Context, project string, opts ...storageOption) (string, error) {
	ctx, record := mc.metrics.startOperation(ctx, "GetServiceAccount", mc.isHTTP)
	res, err := mc.storageClient.GetServiceAccount(ctx, project, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) CreateBucket(ctx context.Context, project, bucket string, attrs *BucketAttrs, enableObjectRetention *bool, opts ...storageOption) (*BucketAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "CreateBucket", mc.isHTTP)
	res, err := mc.storageClient.CreateBucket(ctx, project, bucket, attrs, enableObjectRetention, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) ListBuckets(ctx context.Context, project string, opts ...storageOption) *BucketIterator {
	ctx, _ = mc.metrics.startOperation(ctx, "ListBuckets", mc.isHTTP)
	return mc.storageClient.ListBuckets(ctx, project, opts...)
}

func (mc *metricsStorageClient) DeleteBucket(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteBucket", mc.isHTTP)
	err := mc.storageClient.DeleteBucket(ctx, bucket, conds, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) GetBucket(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) (*BucketAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "GetBucket", mc.isHTTP)
	res, err := mc.storageClient.GetBucket(ctx, bucket, conds, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) UpdateBucket(ctx context.Context, bucket string, uattrs *BucketAttrsToUpdate, conds *BucketConditions, opts ...storageOption) (*BucketAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "UpdateBucket", mc.isHTTP)
	res, err := mc.storageClient.UpdateBucket(ctx, bucket, uattrs, conds, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) LockBucketRetentionPolicy(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "LockBucketRetentionPolicy", mc.isHTTP)
	err := mc.storageClient.LockBucketRetentionPolicy(ctx, bucket, conds, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) ListObjects(ctx context.Context, bucket string, q *Query, opts ...storageOption) *ObjectIterator {
	ctx, _ = mc.metrics.startOperation(ctx, "ListObjects", mc.isHTTP)
	return mc.storageClient.ListObjects(ctx, bucket, q, opts...)
}

func (mc *metricsStorageClient) DeleteObject(ctx context.Context, bucket, object string, gen int64, conds *Conditions, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteObject", mc.isHTTP)
	err := mc.storageClient.DeleteObject(ctx, bucket, object, gen, conds, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) GetObject(ctx context.Context, params *getObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "GetObject", mc.isHTTP)
	res, err := mc.storageClient.GetObject(ctx, params, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) UpdateObject(ctx context.Context, params *updateObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "UpdateObject", mc.isHTTP)
	res, err := mc.storageClient.UpdateObject(ctx, params, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) RestoreObject(ctx context.Context, params *restoreObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "RestoreObject", mc.isHTTP)
	res, err := mc.storageClient.RestoreObject(ctx, params, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) MoveObject(ctx context.Context, params *moveObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "MoveObject", mc.isHTTP)
	res, err := mc.storageClient.MoveObject(ctx, params, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) ComposeObject(ctx context.Context, req *composeObjectRequest, opts ...storageOption) (*ObjectAttrs, error) {
	ctx, record := mc.metrics.startOperation(ctx, "ComposeObject", mc.isHTTP)
	res, err := mc.storageClient.ComposeObject(ctx, req, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) RewriteObject(ctx context.Context, req *rewriteObjectRequest, opts ...storageOption) (*rewriteObjectResponse, error) {
	ctx, record := mc.metrics.startOperation(ctx, "RewriteObject", mc.isHTTP)
	res, err := mc.storageClient.RewriteObject(ctx, req, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) NewRangeReader(ctx context.Context, params *newRangeReaderParams, opts ...storageOption) (*Reader, error) {
	ctx, record := mc.metrics.startOperation(ctx, "ReadObject", mc.isHTTP)
	r, err := mc.storageClient.NewRangeReader(ctx, params, opts...)
	if err != nil {
		record(err)
		return nil, err
	}
	if state := metricsStateFromContext(ctx); state != nil {
		r.metricsState = state
	}
	return r, nil
}

func (mc *metricsStorageClient) OpenWriter(params *openWriterParams, opts ...storageOption) (internalWriter, error) {
	ctx, _ := mc.metrics.startOperation(params.ctx, "WriteObject", mc.isHTTP)
	params.ctx = ctx
	return mc.storageClient.OpenWriter(params, opts...)
}

func (mc *metricsStorageClient) NewMultiRangeDownloader(ctx context.Context, params *newMultiRangeDownloaderParams, opts ...storageOption) (*MultiRangeDownloader, error) {
	ctx, _ = mc.metrics.startOperation(ctx, "ReadObject", mc.isHTTP)
	return mc.storageClient.NewMultiRangeDownloader(ctx, params, opts...)
}

func (mc *metricsStorageClient) GetIamPolicy(ctx context.Context, resource string, version int32, opts ...storageOption) (*iampb.Policy, error) {
	ctx, record := mc.metrics.startOperation(ctx, "GetIamPolicy", mc.isHTTP)
	res, err := mc.storageClient.GetIamPolicy(ctx, resource, version, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) SetIamPolicy(ctx context.Context, resource string, policy *iampb.Policy, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "SetIamPolicy", mc.isHTTP)
	err := mc.storageClient.SetIamPolicy(ctx, resource, policy, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) TestIamPermissions(ctx context.Context, resource string, permissions []string, opts ...storageOption) ([]string, error) {
	ctx, record := mc.metrics.startOperation(ctx, "TestIamPermissions", mc.isHTTP)
	res, err := mc.storageClient.TestIamPermissions(ctx, resource, permissions, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) GetHMACKey(ctx context.Context, project, accessID string, opts ...storageOption) (*HMACKey, error) {
	ctx, record := mc.metrics.startOperation(ctx, "GetHMACKey", mc.isHTTP)
	res, err := mc.storageClient.GetHMACKey(ctx, project, accessID, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) ListHMACKeys(ctx context.Context, project string, serviceAccountEmail string, showDeletedKeys bool, opts ...storageOption) *HMACKeysIterator {
	ctx, _ = mc.metrics.startOperation(ctx, "ListHMACKeys", mc.isHTTP)
	return mc.storageClient.ListHMACKeys(ctx, project, serviceAccountEmail, showDeletedKeys, opts...)
}

func (mc *metricsStorageClient) UpdateHMACKey(ctx context.Context, project, serviceAccountEmail, accessID string, attrs *HMACKeyAttrsToUpdate, opts ...storageOption) (*HMACKey, error) {
	ctx, record := mc.metrics.startOperation(ctx, "UpdateHMACKey", mc.isHTTP)
	res, err := mc.storageClient.UpdateHMACKey(ctx, project, serviceAccountEmail, accessID, attrs, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) CreateHMACKey(ctx context.Context, project, serviceAccountEmail string, opts ...storageOption) (*HMACKey, error) {
	ctx, record := mc.metrics.startOperation(ctx, "CreateHMACKey", mc.isHTTP)
	res, err := mc.storageClient.CreateHMACKey(ctx, project, serviceAccountEmail, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) DeleteHMACKey(ctx context.Context, project, accessID string, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteHMACKey", mc.isHTTP)
	err := mc.storageClient.DeleteHMACKey(ctx, project, accessID, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) ListNotifications(ctx context.Context, bucket string, opts ...storageOption) (map[string]*Notification, error) {
	ctx, record := mc.metrics.startOperation(ctx, "ListNotifications", mc.isHTTP)
	res, err := mc.storageClient.ListNotifications(ctx, bucket, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) CreateNotification(ctx context.Context, bucket string, n *Notification, opts ...storageOption) (*Notification, error) {
	ctx, record := mc.metrics.startOperation(ctx, "CreateNotification", mc.isHTTP)
	res, err := mc.storageClient.CreateNotification(ctx, bucket, n, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) DeleteNotification(ctx context.Context, bucket string, id string, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteNotification", mc.isHTTP)
	err := mc.storageClient.DeleteNotification(ctx, bucket, id, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) DeleteDefaultObjectACL(ctx context.Context, bucket string, entity ACLEntity, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteDefaultObjectACL", mc.isHTTP)
	err := mc.storageClient.DeleteDefaultObjectACL(ctx, bucket, entity, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) ListDefaultObjectACLs(ctx context.Context, bucket string, opts ...storageOption) ([]ACLRule, error) {
	ctx, record := mc.metrics.startOperation(ctx, "ListDefaultObjectACLs", mc.isHTTP)
	res, err := mc.storageClient.ListDefaultObjectACLs(ctx, bucket, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) UpdateDefaultObjectACL(ctx context.Context, bucket string, entity ACLEntity, role ACLRole, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "UpdateDefaultObjectACL", mc.isHTTP)
	err := mc.storageClient.UpdateDefaultObjectACL(ctx, bucket, entity, role, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) DeleteBucketACL(ctx context.Context, bucket string, entity ACLEntity, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteBucketACL", mc.isHTTP)
	err := mc.storageClient.DeleteBucketACL(ctx, bucket, entity, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) ListBucketACLs(ctx context.Context, bucket string, opts ...storageOption) ([]ACLRule, error) {
	ctx, record := mc.metrics.startOperation(ctx, "ListBucketACLs", mc.isHTTP)
	res, err := mc.storageClient.ListBucketACLs(ctx, bucket, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) UpdateBucketACL(ctx context.Context, bucket string, entity ACLEntity, role ACLRole, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "UpdateBucketACL", mc.isHTTP)
	err := mc.storageClient.UpdateBucketACL(ctx, bucket, entity, role, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) DeleteObjectACL(ctx context.Context, bucket, object string, entity ACLEntity, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "DeleteObjectACL", mc.isHTTP)
	err := mc.storageClient.DeleteObjectACL(ctx, bucket, object, entity, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) ListObjectACLs(ctx context.Context, bucket, object string, opts ...storageOption) ([]ACLRule, error) {
	ctx, record := mc.metrics.startOperation(ctx, "ListObjectACLs", mc.isHTTP)
	res, err := mc.storageClient.ListObjectACLs(ctx, bucket, object, opts...)
	record(err)
	return res, err
}

func (mc *metricsStorageClient) UpdateObjectACL(ctx context.Context, bucket, object string, entity ACLEntity, role ACLRole, opts ...storageOption) error {
	ctx, record := mc.metrics.startOperation(ctx, "UpdateObjectACL", mc.isHTTP)
	err := mc.storageClient.UpdateObjectACL(ctx, bucket, object, entity, role, opts...)
	record(err)
	return err
}

func (mc *metricsStorageClient) Close() error {
	return mc.storageClient.Close()
}

func (mc *metricsStorageClient) fetchBucketMetadata(ctx context.Context, bucket string) (string, string, error) {
	return mc.storageClient.fetchBucketMetadata(ctx, bucket)
}
