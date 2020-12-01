package ingester

import (
	fmt "fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
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
			reader.xs = append(reader.xs, rec.encodeEntries(nil))
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
