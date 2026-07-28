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

package internal

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/oauth2/google"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/status"

	"cloud.google.com/go/bigtable/internal/directaccess"
	btopt "cloud.google.com/go/bigtable/internal/option"
	gcpmetadata "cloud.google.com/go/compute/metadata"
)

// xdsCdsURITemplate names the Multi-Region AFE frontend pool. We send the
// request to the same client region.
const xdsCdsURITemplate = "xdstp://traffic-director-c2p.xds.googleapis.com/envoy.config.cluster.v3.Cluster/%s-bigtable.googleapis.com/eds_cluster"

// DirectAccessChecker decides whether the runtime environment is compatible
// with Direct Access (DirectPath / DirectPathXds) and provides the dialer used
// for direct-access connections once compatibility is confirmed.
//
// Implementations differ in HOW compatibility is determined:
//   - pingAndWarmDirectAccessChecker probes via a PingAndWarm call at startup
//     (today's only behavior, used by the classic channel pool factory).
//   - A future GetClientConfiguration-based checker (for session-based pools)
//     will derive compatibility from a server-driven configuration response
//     surfaced by ClientConfigurationManager.
type DirectAccessChecker interface {
	// CheckCompatibility reports whether Direct Access is compatible. When
	// compatible, conn is a primed connection the pool may adopt as its first
	// direct-access connection; otherwise conn is nil and the checker has
	// already closed any probe connection it created. Implementations are
	// responsible for recording the direct_access/compatible metric.
	CheckCompatibility(ctx context.Context) (conn *BigtableConn, compatible bool)

	// Dialer returns the dialer used to create direct-access connections
	// after CheckCompatibility has reported compatibility. Only consulted
	// when CheckCompatibility returned true.
	Dialer() func() (*BigtableConn, error)
}

// newDirectAccessEligibleGauge constructs the direct_access/compatible gauge
// from the provided meter provider. Returns nil if meterProvider is nil or
// instrument creation fails — callers treat a nil gauge as a metric-disabled
// no-op so behavior matches the prior in-pool gauge construction.
func newDirectAccessEligibleGauge(meterProvider metric.MeterProvider, logger *log.Logger) metric.Int64Gauge {
	if meterProvider == nil {
		return nil
	}
	gauge, err := meterProvider.Meter(clientMeterName).Int64Gauge(
		"direct_access/compatible",
		metric.WithDescription("Reports 1 if the environment is eligible for DirectPath, 0 otherwise. Based on a connection attempt at startup."),
		metric.WithUnit("1"),
	)
	if err != nil {
		btopt.Debugf(logger, "bigtable_direct_access: failed to create direct_access/compatible metric: %v", err)
		return nil
	}
	return gauge
}

// pingAndWarmDirectAccessChecker probes Direct Access compatibility by
// dialing once and issuing PingAndWarm + ALTS inspection. On failure it
// kicks off an asynchronous investigation that records a more specific
// failure reason on the direct_access/compatible metric.
//
// The checker reuses the same ChannelPrimer that the pool's connection
// factory uses, so the exact PingAndWarm invocation (instance name, app
// profile, feature-flag metadata) stays in one place — both the
// compatibility probe and any single-endpoint investigation go through it.
type pingAndWarmDirectAccessChecker struct {
	dialer          func() (*BigtableConn, error)
	primer          ChannelPrimer
	daEligibleGauge metric.Int64Gauge
	logger          *log.Logger
}

// newPingAndWarmDirectAccessChecker constructs the today-default checker.
// A nil meterProvider produces a checker that silently skips metric
// reporting. The primer must be non-nil; the channel pool factory
// constructs a single ChannelPrimer and shares it with both the pool (via
// WithChannelPrimer) and this checker.
func newPingAndWarmDirectAccessChecker(
	dialer func() (*BigtableConn, error),
	primer ChannelPrimer,
	meterProvider metric.MeterProvider,
	logger *log.Logger,
) *pingAndWarmDirectAccessChecker {
	return &pingAndWarmDirectAccessChecker{
		dialer:          dialer,
		primer:          primer,
		daEligibleGauge: newDirectAccessEligibleGauge(meterProvider, logger),
		logger:          logger,
	}
}

// Dialer returns the configured direct-access dialer.
func (c *pingAndWarmDirectAccessChecker) Dialer() func() (*BigtableConn, error) {
	return c.dialer
}

// CheckCompatibility opens a single probe connection, primes it, and decides
// whether Direct Access is usable. On compatible: the primed connection is
// returned so the pool can adopt it as its first connection (saving one
// redial). On incompatible: any probe connection is closed and an async
// investigation begins to report a specific failure reason.
func (c *pingAndWarmDirectAccessChecker) CheckCompatibility(ctx context.Context) (*BigtableConn, bool) {
	conn, err := c.dialer()
	if err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: dial failed: %v", err)
		return nil, false
	}

	err = c.primer.Prime(ctx, conn)
	if err != nil {
		// PermissionDenied is expected on probes that are otherwise healthy
		// (the bootstrap credentials may lack PingAndWarm), so fall through
		// to the ALTS check rather than failing fast.
		if status.Code(err) != codes.PermissionDenied {
			btopt.Debugf(c.logger, "bigtable_direct_access: Prime() failed during compatibility check: %v", err)
			conn.Close()
			go c.investigateFailure(err)
			return nil, false
		}
		btopt.Debugf(c.logger, "bigtable_direct_access: Prime() failed with PermissionDenied, continuing to ALTS check: %v", err)
	}

	if conn.isALTSConn.Load() {
		c.reportSuccess(ctx, conn.ipProtocol())
		return conn, true
	}

	conn.Close()
	go c.investigateFailure(err)
	return nil, false
}

// reportSuccess records a direct_access/compatible=1 reading.
func (c *pingAndWarmDirectAccessChecker) reportSuccess(ctx context.Context, ipPreference string) {
	if c.daEligibleGauge == nil {
		return
	}
	c.daEligibleGauge.Record(ctx, 1, metric.WithAttributes(
		attribute.String("ip_preference", ipPreference),
		attribute.String("reason", ""),
	))
}

// reportFailure records a direct_access/compatible=0 reading with the given
// reason tag (e.g. "manually_disabled", "metadata_unreachable").
func (c *pingAndWarmDirectAccessChecker) reportFailure(reason string) {
	if c.daEligibleGauge == nil {
		return
	}
	c.daEligibleGauge.Record(context.Background(), 0, metric.WithAttributes(
		attribute.String("ip_preference", ""),
		attribute.String("reason", reason),
	))
}

// investigateFailure runs asynchronously after a failed compatibility check
// to determine why Direct Access was not usable, and reports the specific
// reason to the metric. It walks the GCE-environment preconditions in order
// of cheapness — short-circuits as soon as a failing precondition is found.
func (c *pingAndWarmDirectAccessChecker) investigateFailure(originalErr error) {
	if err := directaccess.IsRunningOnGCP(); err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: %v. Original error: %v", err, originalErr)
		c.reportFailure("not_in_gcp")
		return
	}

	if err := directaccess.CheckMetadataServerReachability(); err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Metadata unreachable: %v", err)
		c.reportFailure("metadata_unreachable")
		return
	}

	ipv4, errV4 := directaccess.FetchIPFromMetadataServer("IPv4")
	ipv6, errV6 := directaccess.FetchIPFromMetadataServer("IPv6")

	if errV4 != nil && errV6 != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Neither IPv4 nor IPv6 assigned. v4Err: %v, v6Err: %v", errV4, errV6)
		c.reportFailure("no_ip_assigned")
		return
	}

	if err := directaccess.CheckLoopbackInterfaceUp(); err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Loopback interface down: %v", err)
		c.reportFailure("loopback_misconfigured")
		return
	}

	if ipv4 != nil {
		if err := directaccess.CheckLocalIPv4LoopbackAddress(); err != nil {
			btopt.Debugf(c.logger, "bigtable_direct_access: investigation: IPv4 loopback missing: %v", err)
			c.reportFailure("loopback_misconfigured_ipv4")
			return
		}
	}

	if ipv6 != nil {
		if err := directaccess.CheckLocalIPv6LoopbackAddress(); err != nil {
			btopt.Debugf(c.logger, "bigtable_direct_access: investigation: IPv6 loopback missing: %v", err)
			c.reportFailure("loopback_misconfigured_ipv6")
			return
		}
	}

	v4Plumbed, v6Plumbed := checkIPPlumbing(c.logger, ipv4, ipv6)

	// If metadata assigned IPs but the guest OS hasn't plumbed any of them onto
	// an interface, that's acceptable for GKE pods — fall through to the xDS
	// check which will rely on kernel default routing.
	if !v4Plumbed && !v6Plumbed {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Metadata IPs not plumbed to local interfaces (likely containerized). Relying on kernel default routing.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zone, zoneErr := gcpmetadata.ZoneWithContext(ctx)
	instanceID, idErr := gcpmetadata.InstanceIDWithContext(ctx)
	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Metadata fetch - Zone: %q (err: %v), InstanceID: %q (err: %v)", zone, zoneErr, instanceID, idErr)

	if zoneErr != nil || idErr != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Skipping xDS checks (failed to fetch zone or instanceID)")
		c.reportFailure("metadata_missing")
		return
	}

	region := zone
	if lastDash := strings.LastIndex(zone, "-"); lastDash != -1 {
		region = zone[:lastDash]
	}

	cdsURI := fmt.Sprintf(xdsCdsURITemplate, region)
	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Checking xDS reachability for Node %s in region %s using URI: %s", instanceID, region, cdsURI)

	endpoints, failReason, err := directaccess.FetchXdsEndpoints(ctx, instanceID, zone, cdsURI)
	if err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: xDS check failed: %v", err)
		c.reportFailure(failReason)
		return
	}

	// FetchXdsEndpoints ensures endpoints is non-empty.
	endpoint := endpoints[0]
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Failed to split xDS endpoint host/port %q: %v", endpoint, err)
		c.reportFailure("xds_malformed_endpoint")
		return
	}

	if err := checkKernelRoutes(ipv4, ipv6, v4Plumbed, v6Plumbed, host, endpoint); err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Kernel route check failed to %s: %v", endpoint, err)
		c.reportFailure("route_unreachable")
		return
	}

	if err := c.probeSingleEndpoint(ctx, endpoint); err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: investigation: End-to-end ALTS probe failed: %v", err)
		c.reportFailure("alts_handshake_failed")
		return
	}

	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: All preconditions passed but Direct Access originally failed. Original error: %v", originalErr)
	c.reportFailure("unknown")
}

// probeSingleEndpoint attempts an ALTS-authenticated Prime() request directly
// against a specific xDS endpoint, isolating whether the failure was at the
// load-balancer level vs the endpoint itself.
func (c *pingAndWarmDirectAccessChecker) probeSingleEndpoint(ctx context.Context, targetEndpoint string) error {
	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Creating ALTS channel to %s...", targetEndpoint)

	altsCreds := alts.NewClientCreds(alts.DefaultClientOptions())
	scopes := []string{
		"https://www.googleapis.com/auth/bigtable.data",
		"https://www.googleapis.com/auth/cloud-platform",
	}

	googleCreds, err := google.FindDefaultCredentials(ctx, scopes...)
	if err != nil {
		return fmt.Errorf("failed to find default credentials for probe: %w", err)
	}

	perRPCCreds := oauth.TokenSource{TokenSource: googleCreds.TokenSource}

	// ALTS requires an explicit authority because the server name is what it
	// authenticates against; without it the handshake fails.
	conn, err := grpc.NewClient(targetEndpoint,
		grpc.WithTransportCredentials(altsCreds),
		grpc.WithPerRPCCredentials(perRPCCreds),
		grpc.WithAuthority("bigtable.googleapis.com"),
	)
	if err != nil {
		return fmt.Errorf("grpc.NewClient failed for %s: %w", targetEndpoint, err)
	}
	defer conn.Close()

	btc := NewBigtableConn(conn)

	primeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Executing Prime() on %s...", targetEndpoint)
	if err := c.primer.Prime(primeCtx, btc); err != nil {
		return fmt.Errorf("Prime() failed: %w", err)
	}

	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Prime() SUCCESS on %s!", targetEndpoint)
	return nil
}

// disabledDirectAccessChecker refuses every compatibility check and records a
// single manually_disabled reading the first (and only) time CheckCompatibility
// runs. The factory wires it up when isDirectAccessEnabled returns false so the
// direct_access/compatible metric still surfaces the "off" state — keeping
// observability identical to the pre-modularization behavior in
// NewBigtableChannelPool.
type disabledDirectAccessChecker struct {
	daEligibleGauge metric.Int64Gauge
}

// newDisabledDirectAccessChecker constructs the always-disabled checker. The
// manually_disabled metric is recorded lazily inside CheckCompatibility so it
// uses the pool's ctx for propagation and keeps construction side-effect-free.
func newDisabledDirectAccessChecker(meterProvider metric.MeterProvider, logger *log.Logger) *disabledDirectAccessChecker {
	return &disabledDirectAccessChecker{
		daEligibleGauge: newDirectAccessEligibleGauge(meterProvider, logger),
	}
}

// CheckCompatibility always returns (nil, false) and records the
// manually_disabled metric exactly once — the pool calls it exactly once at
// startup, with the pool ctx.
func (c *disabledDirectAccessChecker) CheckCompatibility(ctx context.Context) (*BigtableConn, bool) {
	if c.daEligibleGauge != nil {
		c.daEligibleGauge.Record(ctx, 0, metric.WithAttributes(
			attribute.String("ip_preference", ""),
			attribute.String("reason", "manually_disabled"),
		))
	}
	return nil, false
}

// Dialer returns nil; never consulted by the pool because CheckCompatibility
// returns false.
func (c *disabledDirectAccessChecker) Dialer() func() (*BigtableConn, error) {
	return nil
}

// checkIPPlumbing verifies whether the IPs assigned by the metadata server are
// actually plumbed onto a local network interface.
func checkIPPlumbing(logger *log.Logger, ipv4, ipv6 *net.IP) (v4Plumbed, v6Plumbed bool) {
	if ipv4 != nil {
		if _, err := directaccess.CheckLocalIPv4Addresses(ipv4); err == nil {
			v4Plumbed = true
		} else {
			btopt.Debugf(logger, "bigtable_direct_access: investigation: IPv4 assigned by metadata but not found on NIC: %v", err)
		}
	}

	if ipv6 != nil {
		if _, err := directaccess.CheckLocalIPv6Addresses(ipv6); err == nil {
			v6Plumbed = true
		} else {
			btopt.Debugf(logger, "bigtable_direct_access: investigation: IPv6 assigned by metadata but not found on NIC: %v", err)
		}
	}

	return v4Plumbed, v6Plumbed
}

// checkKernelRoutes determines the IP family of the target endpoint and
// verifies a valid route exists to it — used to detect misconfigured routing
// tables before blaming the handshake.
func checkKernelRoutes(ipv4, ipv6 *net.IP, v4Plumbed, v6Plumbed bool, host, endpoint string) error {
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("invalid IP format: %s", host)
	}

	if ip.To4() != nil {
		var srcIP *net.IP
		if v4Plumbed {
			srcIP = ipv4
		}
		return directaccess.CheckLocalIPv4Routes(srcIP, endpoint)
	}

	var srcIP *net.IP
	if v6Plumbed {
		srcIP = ipv6
	}
	return directaccess.CheckLocalIPv6Routes(srcIP, endpoint)
}
