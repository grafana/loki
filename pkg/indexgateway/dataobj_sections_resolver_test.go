package indexgateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const resolverTestTenant = "fake"

func TestDataObjectSectionsResolver_Resolve(t *testing.T) {
	from, through := resolverTestWindow()
	ctx := resolverTestCtx()

	t.Run("rejects unaligned window", func(t *testing.T) {
		r := newResolver(t, &instrumentedMetastore{Metastore: newTestMetastore(t)}, "unaligned")
		_, err := r.Resolve(ctx, resolverTestTenant, from, from.Add(time.Hour), `{app="foo"}`)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects window not aligned to a 12h boundary", func(t *testing.T) {
		r := newResolver(t, &instrumentedMetastore{Metastore: newTestMetastore(t)}, "misaligned")
		// from is one hour into the window; through is a correct 12h span from that offset.
		off := from.Add(time.Hour)
		_, err := r.Resolve(ctx, resolverTestTenant, off, off.Add(metastore.MetastoreWindowSize), `{app="foo"}`)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects unparseable matchers", func(t *testing.T) {
		r := newResolver(t, &instrumentedMetastore{Metastore: newTestMetastore(t)}, "badmatchers")
		_, err := r.Resolve(ctx, resolverTestTenant, from, through, "not a selector")
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty matchers returns empty without resolving", func(t *testing.T) {
		ms := &instrumentedMetastore{Metastore: newTestMetastore(t)}
		resp, err := newResolver(t, ms, "empty").Resolve(ctx, resolverTestTenant, from, through, "")
		require.NoError(t, err)
		require.Empty(t, resp.Objects)
		require.Zero(t, ms.sectionsCalls.Load())
	})

	t.Run("resolves and caches", func(t *testing.T) {
		backing := newTestMetastore(t)
		backing.seed(t, fooStream("hello"))
		ms := &instrumentedMetastore{Metastore: backing}
		r := newResolver(t, ms, "resolve")

		resp, err := r.Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Objects)             // the seeded object resolved
		require.NotEmpty(t, resp.Objects[0].Sections) // with at least one section

		// A second identical call is served from cache: the metastore is not queried again.
		_, err = r.Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.NoError(t, err)
		require.Equal(t, int32(1), ms.sectionsCalls.Load())
	})

	t.Run("singleflight collapses concurrent calls", func(t *testing.T) {
		backing := newTestMetastore(t)
		backing.seed(t, fooStream("hello"))

		release := make(chan struct{})
		ms := &instrumentedMetastore{
			Metastore:      backing,
			beforeSections: func() { <-release }, // hold the leader in flight so followers join the same singleflight
		}
		// No cache, so only the singleflight can deduplicate.
		r := newDataObjectSectionsResolver(ms, newDataObjectSectionsCache(nil, log.NewNopLogger()), nil, log.NewNopLogger())

		const n = 8
		var wg sync.WaitGroup
		wg.Add(n)
		for range n {
			go func() {
				defer wg.Done()
				_, _ = r.Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
			}()
		}

		require.Eventually(t, func() bool { return ms.getIndexesCalls.Load() == n }, 3*time.Second, time.Millisecond)
		close(release)
		wg.Wait()

		require.Equal(t, int32(1), ms.sectionsCalls.Load())
	})

	t.Run("leader cancellation does not fail waiters", func(t *testing.T) {
		backing := newTestMetastore(t)
		backing.seed(t, fooStream("hello"))

		release := make(chan struct{})
		ms := &instrumentedMetastore{Metastore: backing, beforeSections: func() { <-release }}
		// No cache, so the two calls collapse onto one shared singleflight resolution.
		r := newDataObjectSectionsResolver(ms, newDataObjectSectionsCache(nil, log.NewNopLogger()), nil, log.NewNopLogger())

		leaderCtx, cancelLeader := context.WithCancel(resolverTestCtx())
		go func() { _, _ = r.Resolve(leaderCtx, resolverTestTenant, from, through, `{app="foo"}`) }()

		waiterErr := make(chan error, 1)
		go func() {
			_, e := r.Resolve(resolverTestCtx(), resolverTestTenant, from, through, `{app="foo"}`)
			waiterErr <- e
		}()

		// Both requests have entered the resolver (GetIndexes runs before the singleflight) and are about
		// to collapse onto one leader.
		require.Eventually(t, func() bool { return ms.getIndexesCalls.Load() == 2 }, 3*time.Second, time.Millisecond)
		cancelLeader() // the leader disconnects while the shared Sections call is blocked
		close(release) // let the detached resolution finish

		// The shared work runs on a context detached from the leader, so a waiter on a live context still
		// gets a valid result instead of the leader's cancellation.
		require.NoError(t, <-waiterErr)
	})

	t.Run("GetIndexes error is propagated", func(t *testing.T) {
		ms := &instrumentedMetastore{Metastore: newTestMetastore(t), getIndexesErr: errors.New("boom")}
		_, err := newResolver(t, ms, "getindexes-err").Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.ErrorContains(t, err, "boom")
	})

	t.Run("Sections error is propagated", func(t *testing.T) {
		ms := &instrumentedMetastore{Metastore: newTestMetastore(t), sectionsErr: errors.New("kaboom")}
		_, err := newResolver(t, ms, "sections-err").Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.ErrorContains(t, err, "kaboom")
	})

	t.Run("late data invalidates cache", func(t *testing.T) {
		backing := newTestMetastore(t)
		backing.seed(t, fooStream("first"))
		ms := &instrumentedMetastore{Metastore: backing}
		r := newResolver(t, ms, "latedata")

		_, err := r.Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.NoError(t, err)

		// Late data: a new object lands in the same window, so the index-object set changes.
		backing.seed(t, fooStream("second"))

		_, err = r.Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.NoError(t, err)

		// The changed index-object set changes the key, so the cache did not serve a stale entry.
		require.Equal(t, int32(2), ms.sectionsCalls.Load())
	})

	t.Run("poisoned cache self-heals", func(t *testing.T) {
		backing := newTestMetastore(t)
		backing.seed(t, fooStream("hello"))
		ms := &instrumentedMetastore{Metastore: backing}
		// The cache always returns undecodable bytes; the resolver must treat it as a miss and recompute.
		r := newDataObjectSectionsResolver(ms, newDataObjectSectionsCache(fixedCache{value: []byte("not-a-proto")}, log.NewNopLogger()), nil, log.NewNopLogger())

		resp, err := r.Resolve(ctx, resolverTestTenant, from, through, `{app="foo"}`)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Objects)
		require.Equal(t, int32(1), ms.sectionsCalls.Load())
	})
}

func TestGateway_ResolveDataObjectSections_DisabledReturnsUnimplemented(t *testing.T) {
	g := &Gateway{} // dataObjSections is nil (feature disabled)
	_, err := g.ResolveDataObjectSections(context.Background(), &logproto.ResolveDataObjectSectionsRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestBuildResolveDataObjectSectionsResponse_GroupsAndSorts(t *testing.T) {
	resp := buildResolveDataObjectSectionsResponse([]*metastore.DataobjSectionDescriptor{
		descriptor("z", 2, 9),
		descriptor("a", 5, 1),
		descriptor("a", 1, 2),
	})
	require.Equal(t, []string{"a", "z"}, []string{resp.Objects[0].ObjectPath, resp.Objects[1].ObjectPath})
	require.Equal(t, int64(1), resp.Objects[0].Sections[0].SectionIdx)
	require.Equal(t, int64(5), resp.Objects[0].Sections[1].SectionIdx)
}

// resolverTestWindow is a 12h UTC-aligned window; seeded log data lands inside it.
func resolverTestWindow() (model.Time, model.Time) {
	from := model.TimeFromUnixNano(time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC).UnixNano())
	return from, from.Add(metastore.MetastoreWindowSize)
}

func resolverTestCtx() context.Context {
	return user.InjectOrgID(context.Background(), resolverTestTenant)
}

// testMetastore is a real metastore.ObjectMetastore over an in-memory filesystem bucket. seed writes
// real data objects (logs -> index -> table-of-contents) so GetIndexes and Sections resolve for real.
type testMetastore struct {
	metastore.Metastore
	bucket *filesystem.Bucket
	up     *uploader.Uploader
	cfg    logsobj.BuilderBaseConfig
	log    log.Logger
}

func newTestMetastore(t *testing.T) *testMetastore {
	t.Helper()
	bucket, err := filesystem.NewBucket(t.TempDir())
	require.NoError(t, err)

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          1 << 20,
		TargetObjectSize:        10 << 20,
		TargetSectionSize:       1 << 20,
		BufferSize:              1 << 20,
		SectionStripeMergeLimit: 2,
	}
	ms := metastore.NewObjectMetastore(bucket, metastore.Config{ReadPostingsSections: true}, log.NewNopLogger(), metastore.NewObjectMetastoreMetrics(nil))
	return &testMetastore{Metastore: ms, bucket: bucket, up: uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger()), cfg: cfg, log: log.NewNopLogger()}
}

// seed writes one data object holding streams, indexes it, and registers it in the table of contents.
func (m *testMetastore) seed(t *testing.T, streams ...logproto.Stream) {
	t.Helper()
	ctx := resolverTestCtx()

	logsBuilder, err := logsobj.NewBuilder(logsobj.BuilderConfig{BuilderBaseConfig: m.cfg}, nil, logsobj.NewBuilderMetrics(), m.log, nil)
	require.NoError(t, err)
	for _, s := range streams {
		require.NoError(t, logsBuilder.Append(resolverTestTenant, s, s.Entries[0].Timestamp))
	}
	logsObj, logsCloser, err := logsBuilder.Flush()
	require.NoError(t, err)
	logsPath, err := m.up.Upload(ctx, logsObj)
	require.NoError(t, err)
	require.NoError(t, logsCloser.Close())

	idxBuilder, err := indexobj.NewBuilder(m.cfg, nil)
	require.NoError(t, err)
	calc := index.NewCalculator(idxBuilder)
	logsRO, err := dataobj.FromBucket(ctx, m.bucket, logsPath, 0)
	require.NoError(t, err)
	require.NoError(t, calc.Calculate(ctx, m.log, logsRO, logsPath))
	idxObj, idxCloser, timeRanges, err := calc.Flush()
	require.NoError(t, err)
	idxPath, err := m.up.Upload(ctx, idxObj)
	require.NoError(t, err)
	require.NoError(t, idxCloser.Close())

	require.NoError(t, metastore.NewTableOfContentsWriter(m.bucket, m.log).WriteEntry(ctx, idxPath, timeRanges))
}

// fooStream returns a stream labelled {app="foo"} with one entry inside resolverTestWindow.
func fooStream(line string) logproto.Stream {
	return logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []logproto.Entry{{Timestamp: time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC), Line: line}},
	}
}

// instrumentedMetastore delegates to a real metastore but counts calls and can block or fail on
// demand, so tests can exercise caching, singleflight, and error handling against real resolution.
type instrumentedMetastore struct {
	metastore.Metastore

	sectionsCalls   atomic.Int32
	getIndexesCalls atomic.Int32

	beforeSections func() // optional: called at the start of Sections (e.g. to block)
	sectionsErr    error  // optional: returned instead of resolving
	getIndexesErr  error  // optional: returned instead of listing
}

func (m *instrumentedMetastore) GetIndexes(ctx context.Context, req metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	m.getIndexesCalls.Add(1)
	if m.getIndexesErr != nil {
		return metastore.GetIndexesResponse{}, m.getIndexesErr
	}
	return m.Metastore.GetIndexes(ctx, req)
}

func (m *instrumentedMetastore) Sections(ctx context.Context, req metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	m.sectionsCalls.Add(1)
	if m.beforeSections != nil {
		m.beforeSections()
	}
	if m.sectionsErr != nil {
		return metastore.SectionsResponse{}, m.sectionsErr
	}
	return m.Metastore.Sections(ctx, req)
}

func newResolver(t *testing.T, ms metastore.Metastore, name string) *DataObjectSectionsResolver {
	t.Helper()
	return newDataObjectSectionsResolver(ms, newDataObjectSectionsCache(newTestEmbeddedCache(t, name), log.NewNopLogger()), nil, log.NewNopLogger())
}

func descriptor(path string, section int64, ids ...int64) *metastore.DataobjSectionDescriptor {
	return &metastore.DataobjSectionDescriptor{
		SectionKey: metastore.SectionKey{ObjectPath: path, SectionIdx: section},
		StreamIDs:  ids,
	}
}

func TestResolveOutcome(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	require.Equal(t, "success", resolveOutcome(context.Background(), nil))
	require.Equal(t, "error", resolveOutcome(context.Background(), errors.New("boom")))
	// A caller cancellation (parent ctx done) is "canceled".
	require.Equal(t, "canceled", resolveOutcome(cancelled, context.Canceled))
	// The internal singleflight-guard timeout surfaces as a deadline error on a still-live caller
	// context; it is a server-side failure, not a client cancellation, so it must be "error".
	require.Equal(t, "error", resolveOutcome(context.Background(), context.DeadlineExceeded))
}
