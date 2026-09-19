package limits

import (
	"context"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
)

type mockLimits struct {
	MaxGlobalStreams int
	IngestionRate    float64
}

func (m *mockLimits) MaxGlobalStreamsPerUser(_ string) int {
	if m.MaxGlobalStreams != 0 {
		return m.MaxGlobalStreams
	}
	return 1000
}

func (m *mockLimits) IngestionRateBytes(_ string) float64 {
	if m.IngestionRate != 0 {
		return m.IngestionRate
	}
	return 0
}

func (m *mockLimits) IngestionBurstSizeBytes(_ string) int {
	return 1000
}

func (m *mockLimits) PolicyMaxGlobalStreamsPerUser(_ string, _ string) (int, bool) {
	return 0, false
}

// mockKafka mocks a [kgo.Client]. The zero value is usable.
type mockKafka struct {
	fetches  []kgo.Fetches
	produced []*kgo.Record

	// produceFailer is an (optional) callback executed in [Produce] that
	// can be used to fail producing certain records. If it is non-nil and
	// returns a non-nil error, the record will be failed, and the error
	// be passed to the promise.
	produceFailer func(r *kgo.Record) error

	// Internal, should not be accessed from tests.
	fetchesIdx int
	mtx        sync.Mutex
}

// PollFetches implements [kgo.Client.PollFetches].
func (m *mockKafka) PollFetches(_ context.Context) kgo.Fetches {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.fetchesIdx >= len(m.fetches) {
		return kgo.Fetches{}
	}
	fetches := m.fetches[m.fetchesIdx]
	m.fetchesIdx++
	return fetches
}

// Produce implements [kgo.Client.Produce].
func (m *mockKafka) Produce(
	_ context.Context,
	r *kgo.Record,
	promise func(*kgo.Record, error),
) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	var err error
	// Check if producing the record should fail.
	if m.produceFailer != nil {
		err = m.produceFailer(r)
	}
	if err != nil {
		promise(nil, err)
		return
	}
	m.produced = append(m.produced, r)
	promise(r, nil)
}
