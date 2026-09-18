package indexgateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/dataobjmetrics"
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
		r := newResolver(t, &instrumentedMetastore{Metastore: newTestMetastore(t)})
		_, err := r.Resolve(ctx, from, from.Add(time.Hour), `{app="foo"}`)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects window not aligned to a 12h boundary", func(t *testing.T) {
		r := newResolver(t, &instrumentedMetastore{Metastore: newTestMetastore(t)})
		// from is one hour into the window; through is a correct 12h span from that offset.
		off := from.Add(time.Hour)
		_, err := r.Resolve(ctx, off, off.Add(metastore.MetastoreWindowSize), `{app="foo"}`)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects unparseable matchers", func(t *testing.T) {
		r := newResolver(t, &instrumentedMetastore{Metastore: newTestMetastore(t)})
		_, err := r.Resolve(ctx, from, through, "not a selector")
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty matchers returns empty without resolving", func(t *testing.T) {
		ms := &instrumentedMetastore{Metastore: newTestMetastore(t)}
		r := newResolver(t, ms)
		resp, err := r.Resolve(ctx, from, through, "")
		require.NoError(t, err)
		require.Empty(t, resp.Objects)
		require.Zero(t, ms.sectionsCalls.Load())
	})

	t.Run("resolves and records metrics", func(t *testing.T) {
		backing := newTestMetastore(t)
		backing.seed(t, fooStream("hello"))
		ms := &instrumentedMetastore{Metastore: backing}
		r := newResolver(t, ms)

		resp, err := r.Resolve(ctx, from, through, `{app="foo"}`)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Objects)             // the seeded object resolved
		require.NotEmpty(t, resp.Objects[0].Sections) // with at least one section

		// The duration metric recorded one success outcome.
		require.Equal(t, 1, testutil.CollectAndCount(r.duration, "loki_index_gateway_dataobj_sections_resolve_duration_seconds"))

		// Reading index objects from storage recorded object-store requests under the metastore
		// component. The index-gateway installs the xcap capture, so the metastore's reads are folded
		// into these counters.
		requests := testutil.ToFloat64(r.dataObjMetrics.ObjectStoreRequests.WithLabelValues(dataobjmetrics.ComponentMetastore, dataobjmetrics.OperationGet)) +
			testutil.ToFloat64(r.dataObjMetrics.ObjectStoreRequests.WithLabelValues(dataobjmetrics.ComponentMetastore, dataobjmetrics.OperationGetRange)) +
			testutil.ToFloat64(r.dataObjMetrics.ObjectStoreRequests.WithLabelValues(dataobjmetrics.ComponentMetastore, dataobjmetrics.OperationAttributes))
		require.Positive(t, requests, "the metastore must report object-store requests")

	})

	t.Run("Sections error is propagated", func(t *testing.T) {
		ms := &instrumentedMetastore{Metastore: newTestMetastore(t), sectionsErr: errors.New("kaboom")}
		r := newResolver(t, ms)
		_, err := r.Resolve(ctx, from, through, `{app="foo"}`)
		require.ErrorContains(t, err, "kaboom")
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

// instrumentedMetastore delegates to a real metastore but counts Sections calls and can fail on demand,
// so tests can exercise error handling and the empty-request short-circuit. Caching and singleflight
// now live inside the metastore's Sections, below this wrapper, and are tested in the metastore package.
type instrumentedMetastore struct {
	metastore.Metastore

	sectionsCalls atomic.Int32
	sectionsErr   error // optional: returned instead of resolving
}

func (m *instrumentedMetastore) Sections(ctx context.Context, req metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	m.sectionsCalls.Add(1)
	if m.sectionsErr != nil {
		return metastore.SectionsResponse{}, m.sectionsErr
	}
	return m.Metastore.Sections(ctx, req)
}

func newResolver(t *testing.T, ms metastore.Metastore) *DataObjectSectionsResolver {
	t.Helper()
	return NewDataObjectSectionsResolver(ms, prometheus.NewRegistry(), log.NewNopLogger())
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
