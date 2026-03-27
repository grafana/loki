package distributor

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func newTestInMemoryTee(cap int) (*InMemoryDataObjTee, chan *kgo.Record) {
	ch := make(chan *kgo.Record, cap)
	tee := NewInMemoryDataObjTee(ch, prometheus.NewRegistry(), nil, 5*time.Second)
	return tee, ch
}

func TestInMemoryDataObjTee_Register_AddsPending(t *testing.T) {
	tee, _ := newTestInMemoryTee(10)
	ctx := context.Background()

	streams := []KeyedStream{
		{Stream: logproto.Stream{Labels: `{a="b"}`}},
		{Stream: logproto.Stream{Labels: `{c="d"}`}},
		{Stream: logproto.Stream{Labels: `{e="f"}`}},
	}
	pushTracker := &PushTracker{
		done: make(chan struct{}, 1),
		err:  make(chan error, 1),
	}

	tee.Register(ctx, "tenant", streams, pushTracker)
	assert.EqualValues(t, 3, pushTracker.streamsPending.Load())
}

func TestInMemoryDataObjTee_Duplicate_SendsRecords(t *testing.T) {
	tee, ch := newTestInMemoryTee(100)
	ctx := context.Background()
	tenant := "test-tenant"
	now := time.Now()

	streams := []KeyedStream{
		{
			Stream: logproto.Stream{
				Labels: `{job="test"}`,
				Entries: []logproto.Entry{
					{Timestamp: now, Line: "line1"},
					{Timestamp: now.Add(time.Second), Line: "line2"},
				},
			},
		},
	}
	pushTracker := &PushTracker{
		done: make(chan struct{}, 1),
		err:  make(chan error, 1),
	}
	pushTracker.streamsPending.Store(1)

	tee.Duplicate(ctx, tenant, streams, pushTracker)

	// Wait for at least one record to arrive.
	select {
	case rec := <-ch:
		assert.NotNil(t, rec)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for record")
	}
}

func TestInMemoryDataObjTee_Duplicate_ChannelFull_Timeout(t *testing.T) {
	// Channel capacity 0 forces immediate block.
	reg := prometheus.NewRegistry()
	ch := make(chan *kgo.Record, 0)
	tee := NewInMemoryDataObjTee(ch, reg, nil, 50*time.Millisecond) // short timeout for test

	ctx := context.Background()
	tenant := "test-tenant"
	now := time.Now()

	streams := []KeyedStream{
		{
			Stream: logproto.Stream{
				Labels: `{job="test"}`,
				Entries: []logproto.Entry{
					{Timestamp: now, Line: "drop-me"},
				},
			},
		},
	}

	errCh := make(chan error, 1)
	pushTracker := &PushTracker{
		done: make(chan struct{}, 1),
		err:  errCh,
	}
	pushTracker.streamsPending.Store(1)

	tee.Duplicate(ctx, tenant, streams, pushTracker)

	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for error")
	}

	// Verify reason label is "channel_full".
	require.Equal(t, 1.0, testutil.ToFloat64(tee.streamFailures.WithLabelValues("channel_full")))
}

func TestInMemoryDataObjTee_Reason_Label(t *testing.T) {
	t.Run("channel_full", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		ch := make(chan *kgo.Record, 0)
		tee := NewInMemoryDataObjTee(ch, reg, nil, 10*time.Millisecond)

		ctx := context.Background()
		now := time.Now()
		streams := []KeyedStream{
			{Stream: logproto.Stream{
				Labels:  `{job="test"}`,
				Entries: []logproto.Entry{{Timestamp: now, Line: "x"}},
			}},
		}
		errCh := make(chan error, 1)
		pt := &PushTracker{done: make(chan struct{}, 1), err: errCh}
		pt.streamsPending.Store(1)

		tee.Duplicate(ctx, "t", streams, pt)
		<-errCh

		assert.Equal(t, 1.0, testutil.ToFloat64(tee.streamFailures.WithLabelValues("channel_full")))
		assert.Equal(t, 0.0, testutil.ToFloat64(tee.streamFailures.WithLabelValues("encode_error")))
		assert.Equal(t, 0.0, testutil.ToFloat64(tee.streamFailures.WithLabelValues("timeout")))
	})

	t.Run("timeout", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		ch := make(chan *kgo.Record, 0)
		tee := NewInMemoryDataObjTee(ch, reg, nil, 30*time.Second) // long enough for ctx cancel to win

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		now := time.Now()
		streams := []KeyedStream{
			{Stream: logproto.Stream{
				Labels:  `{job="test"}`,
				Entries: []logproto.Entry{{Timestamp: now, Line: "x"}},
			}},
		}
		errCh := make(chan error, 1)
		pt := &PushTracker{done: make(chan struct{}, 1), err: errCh}
		pt.streamsPending.Store(1)

		tee.Duplicate(ctx, "t", streams, pt)
		<-errCh

		assert.Equal(t, 1.0, testutil.ToFloat64(tee.streamFailures.WithLabelValues("timeout")))
		assert.Equal(t, 0.0, testutil.ToFloat64(tee.streamFailures.WithLabelValues("channel_full")))
	})
}
