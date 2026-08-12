package indexgateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grafana/dskit/gate"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewQueryGate_DisabledAdmitsEverything(t *testing.T) {
	g := newQueryGate(Config{MaxConcurrent: 0}, prometheus.NewRegistry())

	for range 100 {
		require.NoError(t, g.Start(context.Background()))
	}
}

func TestNewQueryGate_RejectsAfterQueueTimeout(t *testing.T) {
	g := newQueryGate(Config{
		MaxConcurrent:             1,
		MaxConcurrentQueueTimeout: 10 * time.Millisecond,
	}, prometheus.NewRegistry())

	require.NoError(t, g.Start(context.Background()))

	err := g.Start(context.Background())
	require.ErrorIs(t, err, gate.ErrGateTimeout)

	g.Done()
	require.NoError(t, g.Start(context.Background()))
}

func TestMapGateError(t *testing.T) {
	err := mapGateError(gate.ErrGateTimeout)
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unavailable, s.Code())

	sentinel := errors.New("boom")
	require.Equal(t, sentinel, mapGateError(sentinel))
}
