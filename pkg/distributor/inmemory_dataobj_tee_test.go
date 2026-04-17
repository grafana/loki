package distributor

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func newTestInMemoryTee(capacity int) (*InMemoryDataObjTee, chan *kgo.Record) {
	ch := make(chan *kgo.Record, capacity)
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
