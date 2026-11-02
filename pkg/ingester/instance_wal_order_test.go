package ingester

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	loki_runtime "github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/validation"
)

type orderedTestWAL struct {
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	counters []int64
}

func (w *orderedTestWAL) Start()                {}
func (w *orderedTestWAL) Stop() error           { return nil }
func (w *orderedTestWAL) IsDiskThrottled() bool { return false }

func (w *orderedTestWAL) Log(record *wal.Record) error {
	w.once.Do(func() {
		close(w.entered)
		<-w.release
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, entries := range record.RefEntries {
		w.counters = append(w.counters, entries.Counter)
	}
	return nil
}

func TestPushWritesEachStreamToWALBeforeUnlock(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	inst, err := newInstance(defaultConfig(), defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, nil, nil, NewStreamRateCalculator(), nil, nil, retention.NewTenantsRetention(limits))
	require.NoError(t, err)

	const streamLabels = `{app="wal-order"}`
	start := time.Now().Add(-5 * time.Minute)
	push := func(line string, ts time.Time) error {
		return inst.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels:  streamLabels,
			Entries: []logproto.Entry{{Timestamp: ts, Line: line}},
		}}})
	}
	require.NoError(t, push("seed", start))

	w := &orderedTestWAL{entered: make(chan struct{}), release: make(chan struct{})}
	inst.wal = w
	firstDone := make(chan error, 1)
	go func() { firstDone <- push("first", start.Add(time.Second)) }()
	select {
	case <-w.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first WAL write did not start")
	}
	defer func() {
		select {
		case <-w.release:
		default:
			close(w.release)
		}
	}()

	stream, ok := inst.streams.Load(streamLabels)
	require.True(t, ok)
	if stream.chunkMtx.TryLock() {
		stream.chunkMtx.Unlock()
		t.Fatal("stream was unlocked before its WAL write completed")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- push("second", start.Add(2*time.Second)) }()
	close(w.release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	w.mu.Lock()
	defer w.mu.Unlock()
	require.Equal(t, []int64{2, 3}, w.counters)
}
