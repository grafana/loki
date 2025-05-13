package limits

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
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

// mockKafka mocks a [kgo.Client].
type mockKafka struct {
	t   *testing.T
	mtx sync.Mutex

	expectedNumRecords int
	numRecords         int
}

func (k *mockKafka) Produce(
	_ context.Context,
	r *kgo.Record,
	promise func(*kgo.Record, error),
) {
	k.mtx.Lock()
	defer k.mtx.Unlock()
	k.numRecords++
	promise(r, nil)
}

func (k *mockKafka) AssertExpectedNumRecords() {
	require.Equal(k.t, k.expectedNumRecords, k.numRecords)
}
