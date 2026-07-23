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
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"cloud.google.com/go/storage/internal"
	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
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

	cm := &clientMetrics{
		provider:                  provider,
		rpcClientCallDuration:     rpcDuration,
		httpClientRequestDuration: httpDuration,
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
			return strconv.FormatInt(statusCode, 10)
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

	if isHTTP && statusCode >= 400 {
		return strconv.FormatInt(statusCode, 10)
	}

	return "UNKNOWN"
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
	if err != nil {
		duration := time.Since(startTime).Seconds()
		rt.metrics.recordHTTP(req.Context(), req, nil, duration, err)
		return nil, err
	}

	if resp.Body != nil {
		resp.Body = &wrappedResponseBody{
			ReadCloser: resp.Body,
			startTime:  startTime,
			req:        req,
			resp:       resp,
			metrics:    rt.metrics,
		}
	} else {
		duration := time.Since(startTime).Seconds()
		rt.metrics.recordHTTP(req.Context(), req, resp, duration, nil)
	}
	return resp, nil
}

type wrappedResponseBody struct {
	io.ReadCloser
	startTime time.Time
	req       *http.Request
	resp      *http.Response
	metrics   *clientMetrics
	recorded  atomic.Bool
}

func (w *wrappedResponseBody) Read(p []byte) (n int, err error) {
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
}

func (w *wrappedClientStream) RecvMsg(m interface{}) error {
	err := w.ClientStream.RecvMsg(m)
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
