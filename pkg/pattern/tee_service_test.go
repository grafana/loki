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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	client.On("PushInternal", mock.Anything, mock.Anything).Return(response, nil)

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
		{HashKey: 123, Stream: *logproto.FromStream(push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: now, Line: "foo1"},
				{Timestamp: now.Add(1 * time.Second), Line: "bar1"},
				{Timestamp: now.Add(2 * time.Second), Line: "baz1"},
			},
		})},
	}, nil)

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: *logproto.FromStream(push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: now.Add(3 * time.Second), Line: "foo2"},
				{Timestamp: now.Add(4 * time.Second), Line: "bar2"},
				{Timestamp: now.Add(5 * time.Second), Line: "baz2"},
			},
		})},
	}, nil)

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 456, Stream: *logproto.FromStream(push.Stream{
			Labels: `{ping="pong"}`,
			Entries: []push.Entry{
				{Timestamp: now.Add(1 * time.Second), Line: "ping"},
				{Timestamp: now.Add(2 * time.Second), Line: "pong"},
			},
		})},
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
		{HashKey: 123, Stream: *logproto.FromStream(push.Stream{
			Labels:  `{foo="bar"}`,
			Entries: []push.Entry{},
		})},
	}, nil)

	tee.Duplicate(ctx, "test-tenant", []distributor.KeyedStream{
		{HashKey: 456, Stream: *logproto.FromStream(push.Stream{
			Labels:  `{ping="pong"}`,
			Entries: []push.Entry{},
		})},
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
	// Reserve and release the nested size, with shared metadata counted once.
	keyed := func(s push.Stream) []distributor.KeyedStream {
		return []distributor.KeyedStream{{HashKey: 123, Stream: *logproto.FromStream(s)}}
	}
	nestedTeed := func(s logproto.InternalStreamAdapter) teedStream {
		return teedStream{hashKey: 123, stream: s, size: s.Size()}
	}
	buffered := func(s push.Stream) teedStream {
		return nestedTeed(*logproto.FromStream(s))
	}

	t.Run("queued rate shards own their entry slices", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			labels string
			owns   bool
		}{
			{name: "unsharded", labels: `{foo="bar"}`},
			{name: "time shard", labels: `{foo="bar", __time_shard__="1_2"}`},
			{name: "rate shard", labels: `{foo="bar", __stream_shard__="0"}`, owns: true},
			{name: "combined", labels: `{foo="bar", __time_shard__="1_2", __stream_shard__="0"}`, owns: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tee, _ := getTestTee(t)
				entries := []push.Entry{{Line: "kept"}, {Line: "other shard"}}
				shard := push.Stream{Labels: tc.labels, Entries: entries[:1:1]}
				streams := keyed(shard)
				source := streams[0].Stream // the distributor's copy
				tee.cfg.TeeConfig.MaxBufferedBytes = buffered(shard).size
				tee.Duplicate(t.Context(), "test", streams, nil)
				require.Len(t, tee.buf["test"], 1)
				queued := tee.buf["test"][0].stream
				require.Equal(t, buffered(shard).stream, queued)
				queuedEntries := queued.ResourceLogs[0].ScopeLogs[0].Entries
				if tc.owns {
					require.NotSame(t, &entries[0], &queuedEntries[0])
					// The distributor's stream still points at its original entries.
					require.Same(t, &entries[0], &source.ResourceLogs[0].ScopeLogs[0].Entries[0])
				} else {
					require.Same(t, &entries[0], &queuedEntries[0])
				}
			})
		}
	})

	t.Run("multiple nested groups preserve metadata precedence", func(t *testing.T) {
		for _, streamLabels := range []string{`{foo="bar"}`, `{foo="bar", __stream_shard__="0"}`} {
			t.Run(streamLabels, func(t *testing.T) {
				tee, client := getTestTee(t)
				at := time.Now()
				nested := logproto.InternalStreamAdapter{Labels: streamLabels, ResourceLogs: []logproto.ResourceLogs{
					{Attrs: []push.LabelAdapter{{Name: "shared", Value: "resource"}}, ScopeLogs: []logproto.ScopeLogs{
						{Attrs: []push.LabelAdapter{{Name: "shared", Value: "scope"}}, Entries: []push.Entry{
							{Timestamp: at, Line: "entry wins", StructuredMetadata: []push.LabelAdapter{{Name: "shared", Value: "entry"}}},
							{Timestamp: at.Add(time.Second), Line: "scope wins"},
						}},
						{Entries: []push.Entry{{Timestamp: at.Add(2 * time.Second), Line: "resource wins"}}},
					}},
					{ScopeLogs: []logproto.ScopeLogs{{Entries: []push.Entry{{Timestamp: at.Add(3 * time.Second), Line: "no shared attributes"}}}}},
				}}
				want := push.Stream{Labels: streamLabels, Entries: []push.Entry{
					{Timestamp: at, Line: "entry wins", StructuredMetadata: []push.LabelAdapter{{Name: "shared", Value: "entry"}}},
					{Timestamp: at.Add(time.Second), Line: "scope wins", StructuredMetadata: []push.LabelAdapter{{Name: "shared", Value: "scope"}}},
					{Timestamp: at.Add(2 * time.Second), Line: "resource wins", StructuredMetadata: []push.LabelAdapter{{Name: "shared", Value: "resource"}}},
					{Timestamp: at.Add(3 * time.Second), Line: "no shared attributes"},
				}}
				streams := []distributor.KeyedStream{{HashKey: 123, Stream: nested}}
				tee.cfg.TeeConfig.MaxBufferedBytes = nested.Size() - 1
				tee.Duplicate(t.Context(), "test", streams, nil)
				require.Empty(t, tee.buf)
				require.Zero(t, tee.bufferedBytes)

				tee.cfg.TeeConfig.MaxBufferedBytes = nested.Size()
				tee.Duplicate(t.Context(), "test", streams, nil)
				require.Equal(t, []teedStream{nestedTeed(nested)}, tee.buf["test"])
				tee.flush()
				select {
				case request := <-tee.flushQueue:
					tee.sendBatch(t.Context(), request)
				default:
					t.Fatal("flush did not enqueue the stream")
				}
				require.NotNil(t, client.req)
				require.Equal(t, []push.Stream{want}, client.req.Streams)
				require.Zero(t, tee.bufferedBytes)
			})
		}
	})

	t.Run("a full flush queue releases rejected rate-shard bytes", func(t *testing.T) {
		tee, client := getTestTee(t)
		tee.flushQueue = make(chan clientRequest, 1)
		shard := push.Stream{Labels: `{foo="bar", __stream_shard__="0"}`, Entries: []push.Entry{{Line: "kept"}}}
		size := buffered(shard).size
		tee.cfg.TeeConfig.MaxBufferedBytes = 2 * size
		tee.Duplicate(t.Context(), "test", keyed(shard), nil)
		tee.flush()
		require.Len(t, tee.flushQueue, 1)
		require.Equal(t, int64(size), tee.bufferedBytes)

		tee.Duplicate(t.Context(), "test", keyed(shard), nil)
		require.Equal(t, int64(2*size), tee.bufferedBytes)
		tee.flush()
		require.Empty(t, tee.buf)
		require.Len(t, tee.flushQueue, 1)
		require.Equal(t, int64(size), tee.bufferedBytes)

		tee.sendBatch(t.Context(), <-tee.flushQueue)
		require.NotNil(t, client.req)
		require.Equal(t, []push.Stream{shard}, client.req.Streams)
		require.Zero(t, tee.bufferedBytes)
		tee.Duplicate(t.Context(), "test", keyed(shard), nil)
		require.Len(t, tee.buf["test"], 1)
		require.Equal(t, int64(size), tee.bufferedBytes)
	})

	t.Run("shared metadata is counted once in the buffer limit and released after flush", func(t *testing.T) {
		ctx := t.Context()
		tee, client := getTestTee(t)
		at := time.Now()
		resourceAttrs := []push.LabelAdapter{{Name: "resource", Value: strings.Repeat("r", 100)}}
		scopeAttrs := []push.LabelAdapter{{Name: "scope", Value: "s"}}
		nested := logproto.InternalStreamAdapter{Labels: `{foo="bar"}`, ResourceLogs: []logproto.ResourceLogs{{
			Attrs: resourceAttrs, ScopeLogs: []logproto.ScopeLogs{{Attrs: scopeAttrs, Entries: []push.Entry{
				{Timestamp: at, Line: "first"}, {Timestamp: at.Add(time.Second), Line: "second"},
			}}},
		}}}
		want := push.Stream{Labels: nested.Labels, Entries: []push.Entry{
			{Timestamp: at, Line: "first", StructuredMetadata: append(append([]push.LabelAdapter(nil), scopeAttrs...), resourceAttrs...)},
			{Timestamp: at.Add(time.Second), Line: "second", StructuredMetadata: append(append([]push.LabelAdapter(nil), scopeAttrs...), resourceAttrs...)},
		}}
		require.Greater(t, want.Size(), nested.Size())
		streams := []distributor.KeyedStream{{HashKey: 123, Stream: nested}}

		tee.cfg.TeeConfig.MaxBufferedBytes = nested.Size() - 1
		tee.Duplicate(ctx, "test", streams, nil)
		require.Empty(t, tee.buf)
		require.Zero(t, tee.bufferedBytes)

		tee.cfg.TeeConfig.MaxBufferedBytes = nested.Size()
		tee.Duplicate(ctx, "test", streams, nil)
		require.Equal(t, int64(nested.Size()), tee.bufferedBytes)
		require.Equal(t, []teedStream{nestedTeed(nested)}, tee.buf["test"])

		tee.flush()
		select {
		case request := <-tee.flushQueue:
			tee.sendBatch(ctx, request)
		default:
			t.Fatal("flush did not enqueue the stream")
		}
		require.NotNil(t, client.req)
		require.Equal(t, []push.Stream{want}, client.req.Streams)
		require.Empty(t, tee.buf)
		require.Zero(t, tee.bufferedBytes)

		tee.Duplicate(ctx, "test", streams, nil)
		require.Equal(t, int64(nested.Size()), tee.bufferedBytes)
		require.Equal(t, []teedStream{nestedTeed(nested)}, tee.buf["test"])
	})

	t.Run("limit is disabled when zero or negative", func(t *testing.T) {
		ctx := t.Context()
		tee, _ := getTestTee(t)

		s1 := push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("abc", 2<<11), // 12 KiB
			}},
		}

		tee.cfg.TeeConfig.MaxBufferedBytes = 0
		tee.Duplicate(ctx, "test", keyed(s1), nil)
		bufferedBytes1 := tee.bufferedBytes
		require.NotZero(t, bufferedBytes1)

		tee.cfg.TeeConfig.MaxBufferedBytes = -1
		tee.Duplicate(ctx, "test", keyed(s1), nil)
		bufferedBytes2 := tee.bufferedBytes
		require.Greater(t, bufferedBytes2, bufferedBytes1)
	})

	t.Run("limit is enforced when positive", func(t *testing.T) {
		ctx := t.Context()
		tee, _ := getTestTee(t)
		tee.cfg.TeeConfig.MaxBufferedBytes = 1024 // 1KB
		require.Len(t, tee.buf, 0)

		// Stream should be accepted, less than 1KB.
		s1 := push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      "abc",
			}},
		}
		require.LessOrEqual(t, s1.Size(), 1024)
		tee.Duplicate(ctx, "test", keyed(s1), nil)
		bufferedBytes1 := tee.bufferedBytes
		require.NotZero(t, bufferedBytes1)
		require.Len(t, tee.buf, 1)
		tenantBuf, ok := tee.buf["test"]
		require.True(t, ok)
		require.Contains(t, tenantBuf, buffered(s1))

		// Stream should be rejected, more than 1KB.
		s2 := push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("d", 1024),
			}},
		}
		require.Greater(t, s2.Size(), 1024)
		tee.Duplicate(ctx, "test", keyed(s2), nil)
		bufferedBytes2 := tee.bufferedBytes
		require.Equal(t, bufferedBytes2, bufferedBytes1)
		require.Len(t, tee.buf, 1)
		// tenantBuf should contain s1, but not s2.
		tenantBuf, ok = tee.buf["test"]
		require.True(t, ok)
		require.Contains(t, tenantBuf, buffered(s1))

		// Stream should be accepted, total of s1 and s3 is less than 1KB.
		s3 := push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("d", 512),
			}},
		}
		tee.Duplicate(ctx, "test", keyed(s3), nil)
		bufferedBytes3 := tee.bufferedBytes
		require.Greater(t, bufferedBytes3, bufferedBytes2)
		require.Len(t, tee.buf, 1)
		// tenantBuf should contain s1 and s3.
		tenantBuf, ok = tee.buf["test"]
		require.True(t, ok)
		require.Contains(t, tenantBuf, buffered(s1))
		require.Contains(t, tenantBuf, buffered(s3))

		// Stream should be rejected, total of s1, s3 and s4 is more than 1KB.
		s4 := push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now(),
				Line:      strings.Repeat("e", 512),
			}},
		}
		tee.Duplicate(ctx, "test", keyed(s4), nil)
		// The total size of s4 is less than s1 and s3, but not 0.
		bufferedBytes4 := tee.bufferedBytes
		require.Equal(t, bufferedBytes4, bufferedBytes3)
		require.Len(t, tee.buf, 1)
		// tenantBuf should contain s1 and s3.
		tenantBuf, ok = tee.buf["test"]
		require.True(t, ok)
		require.Contains(t, tenantBuf, buffered(s1))
		require.Contains(t, tenantBuf, buffered(s3))

		// Flush s1 and s3, s4 should be accepted.
		tee.flush()
		select {
		case clientRequest := <-tee.flushQueue:
			tee.sendBatch(ctx, clientRequest)
		case <-ctx.Done():
			t.Fatal("context canceled before we received request from tee.flushQueue")
		}
		tee.Duplicate(ctx, "test", keyed(s4), nil)
		// The total size of s4 is less than s1 and s3, but not 0.
		bufferedBytes5 := tee.bufferedBytes
		require.Less(t, bufferedBytes5, bufferedBytes4)
		require.NotZero(t, bufferedBytes5)
		require.Len(t, tee.buf, 1)
		// tenantBuf should contain s4.
		tenantBuf, ok = tee.buf["test"]
		require.True(t, ok)
		require.Contains(t, tenantBuf, buffered(s4))
	})

}

func TestPatternTee_PushFallback(t *testing.T) {
	stream := push.Stream{Labels: `{foo="bar"}`, Entries: []push.Entry{{Timestamp: time.Now(), Line: "line"}}}

	for _, tc := range []struct {
		name         string
		internalErr  error
		wantFlatPush bool
	}{
		{name: "pattern ingester supports PushInternal"},
		{name: "pattern ingester predates PushInternal", internalErr: status.Error(codes.Unimplemented, "unknown method"), wantFlatPush: true},
		// Only Unimplemented means the request was not processed.
		{name: "other errors are not resent", internalErr: status.Error(codes.Unavailable, "unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tee, client := getTestTee(t)
			tee.limits = &fakeLimits{} // keep the metrics fallback out of the way
			client.ExpectedCalls = nil
			client.On("PushInternal", mock.Anything, mock.Anything).Return(&logproto.PushResponse{}, tc.internalErr)
			client.On("Push", mock.Anything, mock.Anything).Return(&logproto.PushResponse{}, nil)

			tee.Duplicate(t.Context(), "test", []distributor.KeyedStream{{HashKey: 123, Stream: *logproto.FromStream(stream)}}, nil)
			tee.flush()
			tee.sendBatch(t.Context(), <-tee.flushQueue)

			client.AssertCalled(t, "PushInternal", mock.Anything, mock.Anything)
			if tc.wantFlatPush {
				client.AssertCalled(t, "Push", mock.Anything, mock.Anything)
			} else {
				client.AssertNotCalled(t, "Push", mock.Anything, mock.Anything)
			}
			require.Equal(t, []push.Stream{stream}, client.req.Streams)
		})
	}
}
