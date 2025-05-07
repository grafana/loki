package limits

import (
	"context"
	"sync"
	"testing"

	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/stretchr/testify/require"
)

type mockWAL struct {
	t                    *testing.T
	NumAppendsTotal      int
	ExpectedAppendsTotal int
	mtx                  sync.Mutex
}

func (m *mockWAL) Append(_ context.Context, _ string, _ *proto.StreamMetadata) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.NumAppendsTotal++
	return nil
}

func (m *mockWAL) Close() error {
	return nil
}

func (m *mockWAL) AssertAppendsTotal() {
	require.Equal(m.t, m.ExpectedAppendsTotal, m.NumAppendsTotal)
}
