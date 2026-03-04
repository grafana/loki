package limits

import (
	"context"
	"sync/atomic"

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

// mockKafka mocks a [kgo.Client].
type mockKafka struct {
	fetches        chan kgo.Fetches
	produced       chan *kgo.Record
	producedFailed atomic.Int64

	// produceFailer is an (optional) callback executed in [Produce] that
	// can be used to fail producing certain records. If it is non-nil and
	// returns a non-nil error, the record will be failed, and the error
	// be passed to the promise.
	produceFailer func(r *kgo.Record) error
}

// PollFetches implements [kgo.Client.PollFetches].
func (m *mockKafka) PollFetches(ctx context.Context) kgo.Fetches {
	select {
	case <-ctx.Done():
		return kgo.NewErrFetch(ctx.Err())
	case next := <-m.fetches:
		return next
	}
}

// Produce implements [kgo.Client.Produce].
func (m *mockKafka) Produce(
	_ context.Context,
	r *kgo.Record,
	promise func(*kgo.Record, error),
) {
	// Check if producing the record should fail.
	if m.produceFailer != nil {
		m.producedFailed.Add(1)
		promise(nil, m.produceFailer(r))
		return
	}
	m.produced <- r
	promise(r, nil)
}
