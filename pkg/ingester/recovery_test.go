package ingester

import (
	"context"
	fmt "fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	loki_runtime "github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/validation"
)

type MemoryWALReader struct {
	xs [][]byte

	initialized bool
}

func (m *MemoryWALReader) Next() bool {
	if len(m.xs) < 1 {
		return false
	}

	// don't advance on the first call
	if !m.initialized {
		m.initialized = true
		return true
	}

	m.xs = m.xs[1:]
	return len(m.xs) > 0
}

func (m *MemoryWALReader) Err() error { return nil }

func (m *MemoryWALReader) Record() []byte { return m.xs[0] }

func buildMemoryReader(users, totalStreams, entriesPerStream int) (*MemoryWALReader, []*WALRecord) {
	var recs []*WALRecord
	reader := &MemoryWALReader{}
	for i := 0; i < totalStreams; i++ {
		user := fmt.Sprintf("%d", i%users)
		recs = append(recs, &WALRecord{
			UserID: user,
			Series: []record.RefSeries{
				{
					Ref: uint64(i),
					Labels: labels.FromMap(
						map[string]string{
							"stream": fmt.Sprint(i),
							"user":   user,
						},
					),
				},
			},
		})

		var entries []logproto.Entry
		for j := 0; j < entriesPerStream; j++ {
			entries = append(entries, logproto.Entry{
				Timestamp: time.Unix(int64(j), 0),
				Line:      fmt.Sprintf("%d", j),
			})
		}
		recs = append(recs, &WALRecord{
			UserID: user,
			RefEntries: []RefEntries{
				{
					Ref:     uint64(i),
					Entries: entries,
				},
			},
		})
	}

	for _, rec := range recs {
		if len(rec.Series) > 0 {
			reader.xs = append(reader.xs, rec.encodeSeries(nil))
		}

		if len(rec.RefEntries) > 0 {
			reader.xs = append(reader.xs, rec.encodeEntries(CurrentEntriesRec, nil))
		}
	}

	return reader, recs

}

type MemRecoverer struct {
	users map[string]map[uint64][]logproto.Entry
	done  chan struct{}

	sync.Mutex
	usersCt, streamsCt, seriesCt int
}

func NewMemRecoverer() *MemRecoverer {
	return &MemRecoverer{
		users: make(map[string]map[uint64][]logproto.Entry),
		done:  make(chan struct{}),
	}
}

func (r *MemRecoverer) NumWorkers() int { return runtime.GOMAXPROCS(0) }

func (r *MemRecoverer) Series(_ *Series) error { return nil }

func (r *MemRecoverer) SetStream(userID string, series record.RefSeries) error {
	r.Lock()
	defer r.Unlock()
	user, ok := r.users[userID]
	if !ok {
		user = make(map[uint64][]logproto.Entry)
		r.users[userID] = user
		r.usersCt++
	}

	if _, exists := user[series.Ref]; exists {
		return errors.Errorf("stream (%d) already exists for user (%s)", series.Ref, userID)
	}

	user[series.Ref] = make([]logproto.Entry, 0)
	r.streamsCt++
	return nil
}

func (r *MemRecoverer) Push(userID string, entries RefEntries) error {
	r.Lock()
	defer r.Unlock()

	user, ok := r.users[userID]
	if !ok {
		return errors.Errorf("unexpected user access (%s)", userID)
	}

	stream, ok := user[entries.Ref]
	if !ok {
		return errors.Errorf("unexpected stream access")
	}

	r.seriesCt += len(entries.Entries)
	user[entries.Ref] = append(stream, entries.Entries...)
	return nil
}

func (r *MemRecoverer) Close() { close(r.done) }

func (r *MemRecoverer) Done() <-chan struct{} { return r.done }

func Test_InMemorySegmentRecover(t *testing.T) {
	var (
		users            = 10
		streamsCt        = 1000
		entriesPerStream = 50
	)
	reader, recs := buildMemoryReader(users, streamsCt, entriesPerStream)

	recoverer := NewMemRecoverer()

	require.Nil(t, RecoverWAL(reader, recoverer))
	recoverer.Close()

	require.Equal(t, users, recoverer.usersCt)
	require.Equal(t, streamsCt, recoverer.streamsCt)
	require.Equal(t, streamsCt*entriesPerStream, recoverer.seriesCt)

	for _, rec := range recs {
		user, ok := recoverer.users[rec.UserID]
		require.Equal(t, true, ok)

		for _, s := range rec.Series {
			_, ok := user[s.Ref]
			require.Equal(t, true, ok)
		}

		for _, entries := range rec.RefEntries {
			stream, ok := user[entries.Ref]
			require.Equal(t, true, ok)

			for i, entry := range entries.Entries {
				require.Equal(t, entry, stream[i])
			}
		}
	}

}

func TestSeriesRecoveryNoDuplicates(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	i, err := New(ingesterConfig, client.Config{}, store, limits, loki_runtime.DefaultTenantConfigs(), nil)
	require.NoError(t, err)

	mkSample := func(i int) *logproto.PushRequest {
		return &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					// Note: must use normalized labels here b/c we expect them
					// sorted but use a string for efficiency.
					Labels: `{bar="baz1", foo="bar"}`,
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(int64(i), 0),
							Line:      fmt.Sprintf("line %d", i),
						},
					},
				},
			},
		}
	}

	req := mkSample(1)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, req)
	require.NoError(t, err)

	iter := newIngesterSeriesIter(i).Iter()
	require.Equal(t, true, iter.Next())

	series := iter.Stream()
	require.Equal(t, false, iter.Next())

	// create a new ingester now
	i, err = New(ingesterConfig, client.Config{}, store, limits, loki_runtime.DefaultTenantConfigs(), nil)
	require.NoError(t, err)

	// recover the checkpointed series
	recoverer := newIngesterRecoverer(i)
	require.NoError(t, recoverer.Series(series))

	_, err = i.Push(ctx, req)
	require.NoError(t, err) // we don't error on duplicate pushes

	result := mockQuerierServer{
		ctx: ctx,
	}

	// ensure no duplicate log lines exist
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar",bar="baz1"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(10, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	expected := []logproto.Stream{
		{
			Labels: `{bar="baz1", foo="bar"}`,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(1, 0),
					Line:      "line 1",
				},
			},
		},
	}
	require.Equal(t, expected, result.resps[0].Streams)
}
