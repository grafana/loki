package ingester

import (
	"context"
	fmt "fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"
)

// small util for ensuring data exists as we expect
func ensureIngesterData(ctx context.Context, t *testing.T, start, end time.Time, i *Ingester) {
	result := mockQuerierServer{
		ctx: ctx,
	}
	err := i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar"}`,
		Limit:    100,
		Start:    start,
		End:      end,
	}, &result)

	ln := int(end.Sub(start) / time.Second)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 2)
	require.Len(t, result.resps[0].Streams[0].Entries, ln)
	require.Len(t, result.resps[0].Streams[1].Entries, ln)
}

func defaultIngesterTestConfigWithWAL(t *testing.T, walDir string) Config {
	ingesterConfig := defaultIngesterTestConfig(t)
	ingesterConfig.MaxTransferRetries = 0
	ingesterConfig.WAL = WALConfig{
		Enabled:            true,
		Dir:                walDir,
		Recover:            true,
		CheckpointDuration: time.Second,
	}

	return ingesterConfig
}

func TestIngesterWAL(t *testing.T) {

	walDir, err := ioutil.TempDir(os.TempDir(), "loki-wal")
	require.Nil(t, err)
	defer os.RemoveAll(walDir)

	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
		},
	}

	start := time.Now()
	steps := 10
	end := start.Add(time.Second * time.Duration(steps))

	for i := 0; i < steps; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	ensureIngesterData(ctx, t, start, end, i)

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// ensure we haven't checkpointed yet
	expectCheckpoint(t, walDir, false)

	// restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// ensure we've recovered data from wal segments
	ensureIngesterData(ctx, t, start, end, i)

	time.Sleep(ingesterConfig.WAL.CheckpointDuration + time.Second) // give a bit of buffer
	// ensure we have checkpointed now
	expectCheckpoint(t, walDir, true)

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// ensure we've recovered data from checkpoint+wal segments
	ensureIngesterData(ctx, t, start, end, i)

}

func TestIngesterWALIgnoresStreamLimits(t *testing.T) {
	walDir, err := ioutil.TempDir(os.TempDir(), "loki-wal")
	require.Nil(t, err)
	defer os.RemoveAll(walDir)

	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
		},
	}

	start := time.Now()
	steps := 10
	end := start.Add(time.Second * time.Duration(steps))

	for i := 0; i < steps; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	ensureIngesterData(ctx, t, start, end, i)

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Limit all streams except those written during WAL recovery.
	limitCfg := defaultLimitsTestConfig()
	limitCfg.MaxLocalStreamsPerUser = -1
	limits, err = validation.NewOverrides(limitCfg, nil)
	require.NoError(t, err)

	// restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// ensure we've recovered data from wal segments
	ensureIngesterData(ctx, t, start, end, i)

	req = logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="new"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: start,
						Line:      "hi",
					},
				},
			},
		},
	}

	ctx = user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	// Ensure regular pushes error due to stream limits.
	require.Error(t, err)

}

func expectCheckpoint(t *testing.T, walDir string, shouldExist bool) {
	fs, err := ioutil.ReadDir(walDir)
	require.Nil(t, err)
	var found bool
	for _, f := range fs {
		if _, err := checkpointIndex(f.Name(), false); err == nil {
			found = true
		}
	}

	require.True(t, found == shouldExist)
}
