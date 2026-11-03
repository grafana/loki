// Copyright 2024 Google LLC
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

// Package experimental is a collection of experimental features that might
// have some rough edges to them. Housing experimental features in this package
// results in a user accessing these APIs as `experimental.Foo`, thereby making
// it explicit that the feature is experimental and using them in production
// code is at their own risk.
//
// All APIs in this package are experimental.
package experimental

import (
	"time"

	"cloud.google.com/go/storage/internal"
	"go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/api/option"
)

// WithMetricInterval provides a [option.ClientOption] that may be passed to [storage.NewGRPCClient].
// It sets how often to emit metrics [metric.WithInterval] when using
// [metric.NewPeriodicReader]
// When using Cloud Monitoring interval must be at minimum 1 [time.Minute].
// This option is ignored if WithMeterProvider is also set.
func WithMetricInterval(metricInterval time.Duration) option.ClientOption {
	return internal.WithMetricInterval.(func(time.Duration) option.ClientOption)(metricInterval)
}

// WithMetricExporter provides a [option.ClientOption] that may be passed to [storage.NewGRPCClient].
// Set an alternate client-side metric Exporter to emit metrics through.
// Must implement [metric.Exporter]. This option is ignored if WithMeterProvider is also set.
func WithMetricExporter(ex *metric.Exporter) option.ClientOption {
	return internal.WithMetricExporter.(func(*metric.Exporter) option.ClientOption)(ex)
}

// WithMeterProvider provides a [option.ClientOption] that may be passed to [storage.NewGRPCClient].
// Set an alternate client-side meter provider to emit metrics through.
// This option takes precedence over WithMetricExporter and WithMetricInterval.
func WithMeterProvider(mp *metric.MeterProvider) option.ClientOption {
	return internal.WithMeterProvider.(func(*metric.MeterProvider) option.ClientOption)(mp)
}

// WithReadStallTimeout provides a [option.ClientOption] that may be passed to [storage.NewClient] or [storage.NewGRPCClient].
// It enables the client to retry stalled requests when starting a download from
// Cloud Storage. If the timeout elapses with no response from the server, the request
// is automatically retried.
// The timeout is initially set to ReadStallTimeoutConfig.Min. The client tracks
// latency across all read requests from the client for each bucket accessed, and can
// adjust the timeout higher to the target percentile when latency for request to that
// bucket is high.
// Currently, this is supported for downloads ([storage.NewReader] and
// [storage.NewRangeReader] calls) on the XML API and gRPC API (not implemented for JSON API).
func WithReadStallTimeout(rstc *ReadStallTimeoutConfig) option.ClientOption {
	return internal.WithReadStallTimeout.(func(config *ReadStallTimeoutConfig) option.ClientOption)(rstc)
}

// ReadStallTimeoutConfig defines the timeout which is adjusted dynamically based on
// past observed latencies.
type ReadStallTimeoutConfig struct {
	// Min is the minimum duration of the timeout. The default value is 500ms. Requests
	// taking shorter than this value to return response headers will never time out.
	// In general, you should choose a Min value that is greater than the typical value
	// for the target percentile.
	Min time.Duration

	// TargetPercentile is the percentile to target for the dynamic timeout. The default
	// value is 0.99. At the default percentile, at most 1% of requests will be timed out
	// and retried.
	TargetPercentile float64
}

// WithDirectConnectivityEnforced provides an [option.ClientOption] that may be passed to
// [cloud.google.com/go/storage.NewGRPCClient].
// It sets the gRPC client to use direct path connectivity for all requests and may fail
// if direct path connectivity cannot be established for a request.
func WithDirectConnectivityEnforced() option.ClientOption {
	return internal.WithDirectConnectivityEnforced.(func() option.ClientOption)()
}

// WithDirectPathXdsOverInterconnect provides an [option.ClientOption] that may be passed to
// [cloud.google.com/go/storage.NewGRPCClient].
//
// It instructs the client to attempt DirectPath over Google Cloud Interconnect in on-premises
// or hybrid environments without requiring GCE VM residency.
//
// Behavior and Constraints:
//   - The GOOGLE_CLOUD_ENABLE_DIRECT_PATH_XDS_OVER_INTERCONNECT environment variable overrides
//     this option in both directions (e.g. "true"/"1" to enable, "false"/"0" to disable);
//     unparseable values are ignored.
//   - Requirements: default universe domain (googleapis.com), valid credentials, and that
//     DirectPath is not disabled via GOOGLE_CLOUD_DISABLE_DIRECT_PATH.
//   - This option is supported exclusively for gRPC clients.
//   - Has no effect when using a custom grpc.ClientConn, with the storage emulator, or when
//     the new auth library is disabled (GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB).
func WithDirectPathXdsOverInterconnect() option.ClientOption {
	return internal.WithDirectPathXdsOverInterconnect.(func() option.ClientOption)()
}

// WithOtelMetrics provides an [option.ClientOption] that may be passed to
// [cloud.google.com/go/storage.NewClient] or [cloud.google.com/go/storage.NewGRPCClient].
// It enables client-side OpenTelemetry metrics.
func WithOtelMetrics() option.ClientOption {
	return internal.WithOtelMetrics.(func() option.ClientOption)()
}

// WithOtelDebugMetrics provides an [option.ClientOption] that may be passed to
// [cloud.google.com/go/storage.NewClient] or [cloud.google.com/go/storage.NewGRPCClient].
// It enables debug client-side OpenTelemetry metrics.
func WithOtelDebugMetrics() option.ClientOption {
	return internal.WithOtelDebugMetrics.(func() option.ClientOption)()
}

// WithBufferPool provides an [option.ClientOption] that may be passed to
// [cloud.google.com/go/storage.NewGRPCClient].
// It sets the buffer pool used to allocate memory for parallel uploads.
//
// This option is not supported at the moment.
func WithBufferPool(pool BufferPool) option.ClientOption {
	return internal.WithBufferPool.(func(BufferPool) option.ClientOption)(pool)
}

// WithGRPCKeepalive provides an [option.ClientOption] that may be passed to
// [storage.NewGRPCClient] to configure HTTP/2 keepalive pings on the client's
// gRPC connections.
//
// Use this option when long-running or streaming RPCs (such as object reads and
// writes) may encounter silent network blackholes, dead NAT/firewall mappings,
// or ungraceful peer failures where no TCP FIN or RST is delivered. Without
// keepalive, a stalled connection can hang until the OS TCP retransmission
// timeout expires (typically 15+ minutes on Linux).
//
// When enabled, if an active connection with at least one in-flight RPC receives
// no data or control frames from the peer for [GRPCKeepaliveConfig.Time], the
// client sends an HTTP/2 PING frame and waits up to
// [GRPCKeepaliveConfig.Timeout] for a PING acknowledgement. If no acknowledgement
// arrives in time, the client closes the connection and fails all in-flight RPCs
// on that connection with [google.golang.org/grpc/codes.Unavailable], allowing
// retryable operations to reconnect on a healthy connection within roughly
// Time + Timeout.
//
// Keepalive is disabled by default. Pings are only sent while at least one RPC
// is in flight (PermitWithoutStream is always false) so idle connections do not
// consume the server's ping budget. Because gRPC enforces a minimum Time of 10
// seconds and servers may close connections that ping too frequently, a
// reasonable starting point is Time: 30 * time.Second, Timeout: 20 * time.Second.
//
// Example usage:
//
//	client, err := storage.NewGRPCClient(ctx, experimental.WithGRPCKeepalive(
//		experimental.GRPCKeepaliveConfig{
//			Time:    30 * time.Second,
//			Timeout: 20 * time.Second,
//		},
//	))
//
// This option is only supported for gRPC clients ([storage.NewGRPCClient]) and
// is ignored by HTTP clients ([storage.NewClient]). It takes precedence over
// keepalive parameters passed via [option.WithGRPCDialOption].
func WithGRPCKeepalive(cfg GRPCKeepaliveConfig) option.ClientOption {
	return internal.WithGRPCKeepalive.(func(GRPCKeepaliveConfig) option.ClientOption)(cfg)
}

// GRPCKeepaliveConfig configures client-side HTTP/2 keepalive pings for gRPC
// connections, mirroring the Time and Timeout fields of
// [google.golang.org/grpc/keepalive.ClientParameters]. See [WithGRPCKeepalive].
type GRPCKeepaliveConfig struct {
	// Time is the duration of inactivity (with no frames received on a
	// connection that has an in-flight RPC) after which the client sends an
	// HTTP/2 keepalive ping to check whether the peer transport is still alive.
	//
	// A duration smaller than 10 seconds cannot be set: gRPC enforces a hard
	// minimum ping interval of 10 seconds, and any value less than 10 seconds
	// (including 0) is automatically clamped to 10 seconds by gRPC.
	Time time.Duration

	// Timeout is the duration the client waits for a keepalive ping
	// acknowledgement after sending a ping before closing the connection and
	// failing in-flight RPCs. If 0, gRPC's default of 20 seconds is used.
	Timeout time.Duration
}
