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
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/metric"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc/metadata"

	btopt "cloud.google.com/go/bigtable/internal/option"
)

const (
	// directAccessEnvVar is the user-facing env var that toggles direct
	// access. Its value is intentionally still "CBT_ENABLE_DIRECTPATH" for
	// back-compat — only the Go identifier was renamed to align with the
	// rest of the direct-access naming in this package.
	directAccessEnvVar          = "CBT_ENABLE_DIRECTPATH"
	defaultBigtableConnPoolSize = 10
)

// ChannelPoolConfig has configurations for the channel pool.
type ChannelPoolConfig struct {
	AppProfile                string
	DisableDynamicChannelPool bool
	DisableConnectionRecycler bool
	DisableDirectAccess       bool
}

// ManagedChannelPool encapsulates a connection pool along with its lifecycle monitors.
type ManagedChannelPool struct {
	Pool         gtransport.ConnPool
	Dsm          *DynamicScaleMonitor
	ConnRecycler *ConnectionRecycler
}

// Close stops all associated monitors/recyclers and closes the underlying pool.
func (m ManagedChannelPool) Close() error {
	if m.Dsm != nil {
		m.Dsm.Stop()
	}
	if m.ConnRecycler != nil {
		m.ConnRecycler.Stop()
	}
	if m.Pool != nil {
		return m.Pool.Close()
	}
	return nil
}

// CreateAndStartManagedChannelPool initializes and starts the lifecycle monitors for a classic or session connection pool.
//
// `o` is the full set of base ClientOptions used to dial both the classic
// pool and each per-connection dial inside the Bigtable channel pool.
// `directAccessOptions` is the *separate* set of options that opt a single
// dial in to direct access (DirectPath / DirectPathXds / AllowHardBoundTokens).
// They are kept apart because direct access is per-connection and conditional:
// it is layered on top of `o` only when isDirectAccessEnabled(config) is true,
// and only on the dedicated direct-access dialer — never on the fallback
// dialer used when direct access is disabled or unavailable.
func CreateAndStartManagedChannelPool(
	ctx context.Context,
	project, instance string,
	config ChannelPoolConfig,
	otelMeterProvider metric.MeterProvider,
	o []option.ClientOption,
	directAccessOptions []option.ClientOption,
	directAccessMD metadata.MD,
	clientCreationTimestamp time.Time,
	enableBigtableConnPool bool,
) (ManagedChannelPool, error) {
	var m ManagedChannelPool
	if !enableBigtableConnPool {
		var err error
		m.Pool, err = gtransport.DialPool(ctx, o...)
		return m, err
	}

	pool, err := CreateBigtableChannelPool(ctx, project, instance, config, otelMeterProvider, o, directAccessOptions, directAccessMD, clientCreationTimestamp)
	if err != nil {
		return m, err
	}
	m.Pool = pool

	// DefaultDynamicChannelPoolConfig() returns a valid config today, but we
	// validate here as a guardrail: ValidateDynamicConfig is the single source
	// of truth for what "valid" means, so any future tweak to the defaults
	// (or to the validation rules) is caught at client construction instead
	// of silently misbehaving at runtime.
	if !config.DisableDynamicChannelPool {
		if err := ValidateDynamicConfig(btopt.DefaultDynamicChannelPoolConfig(), defaultBigtableConnPoolSize); err != nil {
			pool.Close()
			return m, fmt.Errorf("invalid DynamicChannelPoolConfig: %w", err)
		}

		m.Dsm = NewDynamicScaleMonitor(btopt.DefaultDynamicChannelPoolConfig(), pool)
		m.Dsm.Start(ctx)
	}

	// connection recycler
	if !config.DisableConnectionRecycler {
		m.ConnRecycler = NewConnectionRecycler(btopt.DefaultConnectionRecycleConfig(), pool)
		m.ConnRecycler.Start(ctx)
	}

	return m, nil
}

// CreateBigtableChannelPool is a helper function to initialize a separate BigtableChannelPool instance.
//
// See CreateAndStartManagedChannelPool for the contract on `o` vs.
// `directAccessOptions`.
func CreateBigtableChannelPool(
	ctx context.Context,
	project, instance string,
	config ChannelPoolConfig,
	otelMeterProvider metric.MeterProvider,
	o []option.ClientOption,
	directAccessOptions []option.ClientOption,
	directAccessMD metadata.MD,
	clientCreationTimestamp time.Time,
) (*BigtableChannelPool, error) {
	uResolver, err := internaloption.NewUnsafeResolver(o...)
	var connPoolSize int
	if err != nil {
		connPoolSize = defaultBigtableConnPoolSize
	} else {
		connPoolSize = uResolver.ResolvedGRPCConnPoolSize()
		if connPoolSize == 0 {
			connPoolSize = defaultBigtableConnPoolSize
		}
	}

	fullInstanceName := fmt.Sprintf("projects/%s/instances/%s", project, instance)

	// Build a single PingAndWarm primer and share it with both the pool's
	// connection factory (via WithChannelPrimer) and the direct-access
	// compatibility checker. Keeping the (instanceName, appProfile,
	// featureFlagsMD) tuple in one place avoids the three-arg drift between
	// the two consumers.
	primer := newPingAndWarmChannelPrimer(fullInstanceName, config.AppProfile, directAccessMD)

	poolOpts := []BigtableChannelPoolOption{
		WithMetricsReporterConfig(btopt.DefaultMetricsReporterConfig()),
		WithMeterProvider(otelMeterProvider),
		WithChannelPrimer(primer),
	}

	// Pluggable Direct Access strategy: the classic channel pool factory uses
	// a PingAndWarm probe; when the env/config disables Direct Access we still
	// wire a "disabled" checker so the direct_access/compatible metric keeps
	// surfacing the off state with reason=manually_disabled. The future
	// session pool factory will swap in a GetClientConfiguration-based checker.
	if isDirectAccessEnabled(config) {
		directAccessDialerOptions := make([]option.ClientOption, len(o))
		copy(directAccessDialerOptions, o)
		directAccessDialerOptions = append(directAccessDialerOptions, directAccessOptions...)
		directAccessDialerOptions = append(directAccessDialerOptions, internaloption.AllowHardBoundTokens("ALTS"))

		directAccessDialer := func() (*BigtableConn, error) {
			grpcConn, err := gtransport.Dial(ctx, directAccessDialerOptions...)
			if err != nil {
				return nil, err
			}
			return NewBigtableConn(grpcConn), nil
		}
		checker := newPingAndWarmDirectAccessChecker(
			directAccessDialer,
			primer,
			otelMeterProvider,
			nil, // logger plumbed by callers once available
		)
		poolOpts = append(poolOpts, WithDirectAccessChecker(checker))
	} else {
		poolOpts = append(poolOpts, WithDirectAccessChecker(newDisabledDirectAccessChecker(otelMeterProvider, nil)))
	}

	return NewBigtableChannelPool(ctx,
		connPoolSize,
		btopt.BigtableLoadBalancingStrategy(),
		func() (*BigtableConn, error) {
			grpcConn, err := gtransport.Dial(ctx, o...)
			if err != nil {
				return nil, err
			}
			return NewBigtableConn(grpcConn), nil
		},
		clientCreationTimestamp,
		poolOpts...,
	)
}

func isDirectAccessEnabled(config ChannelPoolConfig) bool {
	if os.Getenv(directAccessEnvVar) == "" {
		return !config.DisableDirectAccess
	}
	res, _ := strconv.ParseBool(os.Getenv(directAccessEnvVar))
	return res
}
