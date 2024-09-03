package pattern

import (
	"context"
	"flag"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/logproto"

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
		ringClient,
		"test",
		nil,
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	return logsTee, client
}

func TestPatternTeeBasic(t *testing.T) {
	tee, client := getTestTee(t)

	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, tee.Start(ctx))

	now := time.Now()
	tee.Duplicate("test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: now, Line: "foo1"},
				{Timestamp: now.Add(1 * time.Second), Line: "bar1"},
				{Timestamp: now.Add(2 * time.Second), Line: "baz1"},
			},
		}},
	})

	tee.Duplicate("test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: now.Add(3 * time.Second), Line: "foo2"},
				{Timestamp: now.Add(4 * time.Second), Line: "bar2"},
				{Timestamp: now.Add(5 * time.Second), Line: "baz2"},
			},
		}},
	})

	tee.Duplicate("test-tenant", []distributor.KeyedStream{
		{HashKey: 456, Stream: push.Stream{
			Labels: `{ping="pong"}`,
			Entries: []push.Entry{
				{Timestamp: now.Add(1 * time.Second), Line: "ping"},
				{Timestamp: now.Add(2 * time.Second), Line: "pong"},
			},
		}},
	})

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

func TestPatternTeeEmptyStream(t *testing.T) {
	tee, client := getTestTee(t)

	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, tee.Start(ctx))

	tee.Duplicate("test-tenant", []distributor.KeyedStream{
		{HashKey: 123, Stream: push.Stream{
			Labels:  `{foo="bar"}`,
			Entries: []push.Entry{},
		}},
	})

	tee.Duplicate("test-tenant", []distributor.KeyedStream{
		{HashKey: 456, Stream: push.Stream{
			Labels:  `{ping="pong"}`,
			Entries: []push.Entry{},
		}},
	})

	cancel()

	// This should ensure that everything has been flushed and we have no data races below.
	tee.WaitUntilDone()

	req := client.req
	reqCtx := client.ctx

	require.Nil(t, req)
	require.Nil(t, reqCtx)
}
