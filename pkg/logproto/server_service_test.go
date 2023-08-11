// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/cortex/server_service_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package mimir

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"
)

func TestServerStopViaContext(t *testing.T) {
	serv, err := server.New(server.Config{Registerer: prometheus.NewPedanticRegistry()})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	s := NewServerService(serv, func() []services.Service { return nil })
	require.NoError(t, s.StartAsync(ctx))

	// should terminate soon, since context has short timeout
	require.NoError(t, s.AwaitTerminated(context.Background()))
}

func TestServerStopViaShutdown(t *testing.T) {
	serv, err := server.New(server.Config{Registerer: prometheus.NewPedanticRegistry()})
	require.NoError(t, err)

	s := NewServerService(serv, func() []services.Service { return nil })
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), s))

	// Shutting down HTTP/gRPC servers makes Server stop, but ServerService doesn't expect that to happen.
	serv.Shutdown()

	require.Error(t, s.AwaitTerminated(context.Background()))
	require.Equal(t, services.Failed, s.State())
}

func TestServerStopViaStop(t *testing.T) {
	serv, err := server.New(server.Config{Registerer: prometheus.NewPedanticRegistry()})
	require.NoError(t, err)

	s := NewServerService(serv, func() []services.Service { return nil })
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), s))

	serv.Stop()

	// Stop makes Server stop, but ServerService doesn't expect that to happen.
	require.Error(t, s.AwaitTerminated(context.Background()))
	require.Equal(t, services.Failed, s.State())
}
