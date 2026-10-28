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
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	bigtablepb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btopt "cloud.google.com/go/bigtable/internal/option"
)

// getClientConfigRPCTimeout caps the per-attempt GetClientConfiguration probe.
// Kept in line with pingAndWarm's primeRPCTimeout so the two checkers behave
// symmetrically under a stuck backend.
const getClientConfigRPCTimeout = 10 * time.Second

// sessionClientDirectAccessChecker is the session-pool sibling of
// pingAndWarmDirectAccessChecker: same CheckCompatibility flow (dial →
// probe → ALTS check → success or investigate), but issues
// GetClientConfiguration as the probe RPC. Session channel pools do not
// use PingAndWarm — they warm channels via OpenSession on each stream —
// so this checker probes with the RPC session pools already talk on the
// wire, keeping the compatibility check consistent with production
// traffic.
type sessionClientDirectAccessChecker struct {
	dialer          func() (*BigtableConn, error)
	instanceName    string
	appProfileID    string
	featureFlagsMD  metadata.MD
	daEligibleGauge metric.Int64Gauge
	logger          *log.Logger

	// lastProbeConfig captures the ClientConfiguration returned by the
	// startup GetClientConfiguration probe (populated by CheckCompatibility
	// on the compatible + PermissionDenied paths). Exposed via
	// LastProbeConfig() so a downstream ClientConfigurationManager can
	// seed its state from the probe response and skip its own first poll.
	lastProbeConfig atomic.Pointer[bigtablepb.ClientConfiguration]
}

// NewSessionClientDirectAccessChecker constructs the session-pool
// direct-access compatibility checker (GetClientConfiguration-based
// probe). A nil meterProvider produces a checker that silently skips
// metric reporting. Exported so session-client callers under
// internal/session can plug it into their NewBigtableChannelPool
// construction directly.
func NewSessionClientDirectAccessChecker(
	dialer func() (*BigtableConn, error),
	instanceName, appProfileID string,
	featureFlagsMD metadata.MD,
	meterProvider metric.MeterProvider,
	logger *log.Logger,
) DirectAccessChecker {
	return &sessionClientDirectAccessChecker{
		dialer:          dialer,
		instanceName:    instanceName,
		appProfileID:    appProfileID,
		featureFlagsMD:  featureFlagsMD,
		daEligibleGauge: newDirectAccessEligibleGauge(meterProvider, logger),
		logger:          logger,
	}
}

// Dialer returns the configured direct-access dialer.
func (c *sessionClientDirectAccessChecker) Dialer() func() (*BigtableConn, error) {
	return c.dialer
}

// CheckCompatibility opens a single probe connection, issues
// GetClientConfiguration, and decides whether Direct Access is usable.
// Mirrors pingAndWarmDirectAccessChecker.CheckCompatibility: on compatible,
// the primed connection is returned so the pool can adopt it; on
// incompatible, any probe connection is closed and the async investigation
// records a specific failure reason.
func (c *sessionClientDirectAccessChecker) CheckCompatibility(ctx context.Context) (*BigtableConn, bool) {
	conn, err := c.dialer()
	if err != nil {
		btopt.Debugf(c.logger, "bigtable_direct_access: dial failed: %v", err)
		return nil, false
	}

	resp, err := c.probeGetClientConfig(ctx, conn)
	if resp != nil {
		// Even on PermissionDenied (which we fall through to the ALTS
		// check below) the server may not respond with a body — only
		// store when we actually got one.
		c.lastProbeConfig.Store(resp)
	}
	if err != nil {
		// PermissionDenied is expected on probes that are otherwise healthy
		// (bootstrap credentials may lack GetClientConfiguration), so fall
		// through to the ALTS check rather than failing fast.
		if status.Code(err) != codes.PermissionDenied {
			btopt.Debugf(c.logger, "bigtable_direct_access: GetClientConfiguration failed during compatibility check: %v", err)
			conn.Close()
			go c.investigateFailure(err)
			return nil, false
		}
		btopt.Debugf(c.logger, "bigtable_direct_access: GetClientConfiguration failed with PermissionDenied, continuing to ALTS check: %v", err)
	}

	if conn.isALTSConn.Load() {
		c.reportSuccess(ctx, conn.ipProtocol())
		return conn, true
	}

	conn.Close()
	go c.investigateFailure(err)
	return nil, false
}

// probeGetClientConfig issues a single GetClientConfiguration RPC to trigger
// the ALTS handshake and populate the connection's peer info (isALTSConn +
// remoteAddrType). Returns the response body so callers can stash it for
// reuse (e.g., seeding ClientConfigurationManager on start to skip its
// initial poll).
func (c *sessionClientDirectAccessChecker) probeGetClientConfig(ctx context.Context, conn *BigtableConn) (*bigtablepb.ClientConfiguration, error) {
	client := bigtablepb.NewBigtableClient(conn.ClientConn)
	req := &bigtablepb.GetClientConfigurationRequest{
		InstanceName: c.instanceName,
		AppProfileId: c.appProfileID,
	}

	if c.featureFlagsMD.Len() > 0 {
		originalMD, _ := metadata.FromOutgoingContext(ctx)
		ctx = metadata.NewOutgoingContext(ctx, metadata.Join(originalMD, c.featureFlagsMD))
	}

	probeCtx, cancel := context.WithTimeout(ctx, getClientConfigRPCTimeout)
	defer cancel()

	var p peer.Peer
	resp, err := client.GetClientConfiguration(probeCtx, req, grpc.Peer(&p))
	recordProbePeer(conn, &p)
	return resp, err
}

// LastProbeConfig returns the ClientConfiguration returned by the most
// recent CheckCompatibility probe, or nil if the probe never returned a
// body (never ran, or the server responded without one). Provided so a
// downstream ClientConfigurationManager can seed its initial state from
// the probe response and skip its own eager first poll.
func (c *sessionClientDirectAccessChecker) LastProbeConfig() *bigtablepb.ClientConfiguration {
	return c.lastProbeConfig.Load()
}

// reportSuccess records a direct_access/compatible=1 reading.
func (c *sessionClientDirectAccessChecker) reportSuccess(ctx context.Context, ipPreference string) {
	if c.daEligibleGauge == nil {
		return
	}
	c.daEligibleGauge.Record(ctx, 1, metric.WithAttributes(
		attribute.String("ip_preference", ipPreference),
		attribute.String("reason", ""),
	))
}

// reportFailure records a direct_access/compatible=0 reading with the given
// reason tag.
func (c *sessionClientDirectAccessChecker) reportFailure(reason string) {
	if c.daEligibleGauge == nil {
		return
	}
	c.daEligibleGauge.Record(context.Background(), 0, metric.WithAttributes(
		attribute.String("ip_preference", ""),
		attribute.String("reason", reason),
	))
}

// investigateFailure delegates to the shared precondition walk in
// investigateDirectAccessFailure, plugging in a GetClientConfiguration-based
// probeSingleEndpoint so the end-to-end check speaks the same RPC verb the
// session pool uses in production.
func (c *sessionClientDirectAccessChecker) investigateFailure(originalErr error) {
	investigateDirectAccessFailure(c.logger, c.reportFailure, c.probeSingleEndpoint, originalErr)
}

// probeSingleEndpoint dials targetEndpoint over ALTS and issues
// GetClientConfiguration — the direct-endpoint counterpart to the
// load-balanced probe in CheckCompatibility.
func (c *sessionClientDirectAccessChecker) probeSingleEndpoint(ctx context.Context, targetEndpoint string) error {
	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Creating ALTS channel to %s...", targetEndpoint)

	btc, cleanup, err := newAltsProbeChannel(ctx, targetEndpoint)
	if err != nil {
		return err
	}
	defer cleanup()

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: Executing GetClientConfiguration() on %s...", targetEndpoint)
	// Investigation probe response is discarded — this endpoint-isolated
	// re-probe is diagnostic only; the caller cares about the error, not
	// the config body.
	if _, err := c.probeGetClientConfig(probeCtx, btc); err != nil {
		return fmt.Errorf("GetClientConfiguration() failed: %w", err)
	}

	btopt.Debugf(c.logger, "bigtable_direct_access: investigation: GetClientConfiguration() SUCCESS on %s!", targetEndpoint)
	return nil
}

// recordProbePeer copies the ALTS + IP-protocol side effects that
// BigtableConn.Prime performs when the classic checker probes with
// PingAndWarm. Kept adjacent to the GetClientConfiguration probe so both
// checkers land isALTSConn / remoteAddrType observations identically.
func recordProbePeer(bc *BigtableConn, p *peer.Peer) {
	if p == nil {
		return
	}
	if p.Addr != nil {
		if tcpAddr, ok := p.Addr.(*net.TCPAddr); ok {
			if tcpAddr.IP != nil {
				if tcpAddr.IP.To4() != nil {
					bc.remoteAddrType.Store(int32(ipv4))
				} else {
					bc.remoteAddrType.Store(int32(ipv6))
				}
			}
		}
	}
	if p.AuthInfo != nil {
		if _, ok := p.AuthInfo.(alts.AuthInfo); ok {
			bc.isALTSConn.Store(true)
		}
	}
}
