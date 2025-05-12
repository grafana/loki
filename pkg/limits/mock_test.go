package limits

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

type MockLimits struct {
	MaxGlobalStreams int
	IngestionRate    float64
}

func (m *MockLimits) MaxGlobalStreamsPerUser(_ string) int {
	return m.MaxGlobalStreams
}

func (m *MockLimits) IngestionRateBytes(_ string) float64 {
	return m.IngestionRate
}

func (m *MockLimits) IngestionBurstSizeBytes(_ string) int {
	return 1000
}

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
