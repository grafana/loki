package pattern

import (
	"context"
	"flag"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"

	"github.com/grafana/loki/pkg/push"
)

func getTestTee(t *testing.T) (*TeeService, *mockPoolClient) {
	cfg := Config{}
	cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError)) // set up defaults

	cfg.Enabled = true

	response := &logproto.PushResponse{}
	client := &mockPoolClient{}
	client.On("Push", mock.Anything, mock.Anything).Return(response, nil)

	replicationSet := ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Id: "localhost", Addr: "ingester0"},
			{Id: "remotehost", Addr: "ingester1"},
			{Id: "otherhost", Addr: "ingester2"},
		},
	}

	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(replicationSet, nil)

	ringClient := &fakeRingClient{
		poolClient: client,
		ring:       fakeRing,
	}

	logsTee, err := NewTeeService(
		cfg,
		&fakeLimits{
			metricAggregationEnabled: true,
		},
		ringClient,
		runtime.DefaultTenantConfigs(),
		"test",
		nil,
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	return logsTee, client
}

func TestPatternTee_Basic(t *testing.T) {
	tee, client := getTestTee(t)

	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, tee.Start(ctx))

	now := time.Now()
	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: now, Line: "foo1"},
				{Timestamp: now.Add(1 * time.Second), Line: "bar1"},
				{Timestamp: now.Add(2 * time.Second), Line: "baz1"},
			},
		}},
	}, nil)

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: now.Add(3 * time.Second), Line: "foo2"},
				{Timestamp: now.Add(4 * time.Second), Line: "bar2"},
				{Timestamp: now.Add(5 * time.Second), Line: "baz2"},
			},
		}},
	}, nil)

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 456, Stream: push.Stream{
			Labels: `{ping="pong"}`,
			Entries: []push.Entry{
				{Timestamp: now.Add(1 * time.Second), Line: "ping"},
				{Timestamp: now.Add(2 * time.Second), Line: "pong"},
			},
		}},
	}, nil)

	cancel()

	// This should ensure that everything has been flushed and we have no data races below.
	tee.WaitUntilDone()

	req := client.req
	reqCtx := client.ctx

	require.NotNil(t, req)
	tenant, err := user.ExtractOrgID(reqCtx)
	require.NoError(t, err)

	require.Equal(t, "test-tenant", tenant)

	require.Len(t, req.Streams, 3)

	fooBarEntries := []push.Entry{}
	pingPongEntries := []push.Entry{}

	for _, stream := range req.Streams {
		if stream.Labels == `{foo="bar"}` {
			fooBarEntries = append(fooBarEntries, stream.Entries...)
		}

		if stream.Labels == `{ping="pong"}` {
			pingPongEntries = append(pingPongEntries, stream.Entries...)
		}
	}

	slices.SortFunc(fooBarEntries, func(i, j push.Entry) int {
		return i.Timestamp.Compare(j.Timestamp)
	})

	slices.SortFunc(pingPongEntries, func(i, j push.Entry) int {
		return i.Timestamp.Compare(j.Timestamp)
	})

	require.Equal(t, []push.Entry{
		{Timestamp: now, Line: "foo1"},
		{Timestamp: now.Add(1 * time.Second), Line: "bar1"},
		{Timestamp: now.Add(2 * time.Second), Line: "baz1"},
		{Timestamp: now.Add(3 * time.Second), Line: "foo2"},
		{Timestamp: now.Add(4 * time.Second), Line: "bar2"},
		{Timestamp: now.Add(5 * time.Second), Line: "baz2"},
	}, fooBarEntries)

	require.Equal(t, []push.Entry{
		{Timestamp: now.Add(1 * time.Second), Line: "ping"},
		{Timestamp: now.Add(2 * time.Second), Line: "pong"},
	}, pingPongEntries)
}

func TestPatternTee_EmptyStream(t *testing.T) {
	tee, client := getTestTee(t)

	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, tee.Start(ctx))

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: push.Stream{
			Labels:  `{foo="bar"}`,
			Entries: []push.Entry{},
		}},
	}, nil)

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 456, Stream: push.Stream{
			Labels:  `{ping="pong"}`,
			Entries: []push.Entry{},
		}},
	}, nil)

	cancel()

	// This should ensure that everything has been flushed and we have no data races below.
	tee.WaitUntilDone()

	req := client.req
	reqCtx := client.ctx

	require.Nil(t, req)
	require.Nil(t, reqCtx)
}

func TestPatternTee_MaxBufferedBytes(t *testing.T) {
	ctx := t.Context()
	tee, _ := getTestTee(t)
	tee.cfg.TeeConfig.MaxBufferedBytes = 1024 // 1KB
	require.Len(t, tee.buf, 0)

	// Stream should be accepted, less than 1KB.
	s1 := distributor.KeyedStream{
		HashKey: 123,
		Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      "abc",
			}},
		},
	}
	tee.Duplicate(ctx, "test", []distributor.KeyedStream{s1}, nil)
	require.LessOrEqual(t, s1.Stream.Size(), 1024)
	require.Len(t, tee.buf, 1)
	tenantBuf, ok := tee.buf["test"]
	require.True(t, ok)
	require.Contains(t, tenantBuf, s1)

	// Stream should be rejected, more than 1KB.
	s2 := distributor.KeyedStream{
		HashKey: 123,
		Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("d", 1024),
			}},
		},
	}
	tee.Duplicate(ctx, "test", []distributor.KeyedStream{s2}, nil)
	require.Greater(t, s2.Stream.Size(), 1024)
	require.Len(t, tee.buf, 1)
	// tenantBuf should contain s1, but not s2.
	tenantBuf, ok = tee.buf["test"]
	require.True(t, ok)
	require.Contains(t, tenantBuf, s1)

	// Stream should be accepted, total of s1 and s3 is less than 1KB.
	s3 := distributor.KeyedStream{
		HashKey: 123,
		Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("d", 512),
			}},
		},
	}
	tee.Duplicate(ctx, "test", []distributor.KeyedStream{s3}, nil)
	require.Len(t, tee.buf, 1)
	// tenantBuf should contain s1 and s3.
	tenantBuf, ok = tee.buf["test"]
	require.True(t, ok)
	require.Contains(t, tenantBuf, s1, s3)

	// Stream should be rejected, total of s1, s3 and s4 is more than 1KB.
	s4 := distributor.KeyedStream{
		HashKey: 123,
		Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("e", 512),
			}},
		},
	}
	tee.Duplicate(ctx, "test", []distributor.KeyedStream{s4}, nil)
	require.Len(t, tee.buf, 1)
	// tenantBuf should contain s1 and s3.
	tenantBuf, ok = tee.buf["test"]
	require.True(t, ok)
	require.Contains(t, tenantBuf, s1, s3)

	// Flush s1 and s3, s4 should be accepted.
	tee.flush()
	select {
	case clientRequest := <-tee.flushQueue:
		tee.sendBatch(ctx, clientRequest)
	case <-ctx.Done():
		t.Fatal("context canceled before we received request from tee.flushQueue")
	}
	tee.Duplicate(ctx, "test", []distributor.KeyedStream{s4}, nil)
	require.Len(t, tee.buf, 1)
	// tenantBuf should contain s4.
	tenantBuf, ok = tee.buf["test"]
	require.True(t, ok)
	require.Contains(t, tenantBuf, s4)
}
