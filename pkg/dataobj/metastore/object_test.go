package metastore

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const tenantID = "test-tenant"

// Taken from store_test.go -- we need a populated dataobj/builder/metastore to test labels and values
type testDataBuilder struct {
	t      *testing.T
	bucket objstore.Bucket

	builder  *dataobj.Builder
	meta     *Updater
	uploader *uploader.Uploader
}

func (b *testDataBuilder) addStreamAndFlush(labels string, entries ...logproto.Entry) {
	err := b.builder.Append(logproto.Stream{
		Labels:  labels,
		Entries: entries,
	})
	require.NoError(b.t, err)

	buf := bytes.NewBuffer(make([]byte, 0, 1024*1024))
	stats, err := b.builder.Flush(buf)
	require.NoError(b.t, err)

	path, err := b.uploader.Upload(context.Background(), buf)
	require.NoError(b.t, err)

	err = b.meta.Update(context.Background(), path, stats)
	require.NoError(b.t, err)

	b.builder.Reset()
}

func TestLabels(t *testing.T) {
	// filtering criteria
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, len(matchers))
	})
}

func TestNonExistentLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "invalid"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 0)
	})
}

func TestMixedLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "invalid"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 0)
	})
}

func TestValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, len(matchers))
	})
}

func TestNonExistentValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "invalid"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 0)
	})
}

func TestMixedValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 0)
	})
}

func queryMetastore(t *testing.T, tenantID string, mfunc func(context.Context, time.Time, time.Time, Metastore)) {
	now := time.Now()
	start := now.Add(-time.Hour * 5)
	end := now.Add(time.Hour * 5)

	builder := newTestDataBuilder(t, tenantID)
	setupTestData(t, builder)

	mstore := NewObjectMetastore(builder.bucket)
	defer func() {
		require.NoError(t, mstore.bucket.Close())
	}()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	mfunc(ctx, start, end, mstore)
}

func newTestDataBuilder(t *testing.T, tenantID string) *testDataBuilder {
	bucket := objstore.NewInMemBucket()

	builder, err := dataobj.NewBuilder(dataobj.BuilderConfig{
		TargetPageSize:    1024 * 1024,      // 1MB
		TargetObjectSize:  10 * 1024 * 1024, // 10MB
		TargetSectionSize: 1024 * 1024,      // 1MB
		BufferSize:        1024 * 1024,      // 1MB
	})
	require.NoError(t, err)

	meta := NewUpdater(bucket, tenantID, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, meta.RegisterMetrics(prometheus.NewPedanticRegistry()))

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID)
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	return &testDataBuilder{
		t:        t,
		bucket:   bucket,
		builder:  builder,
		meta:     meta,
		uploader: uploader,
	}
}

func setupTestData(t *testing.T, builder *testDataBuilder) time.Time {
	t.Helper()
	now := time.Now()
	builder.addStreamAndFlush(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now.Add(-2 * time.Hour), Line: "foo_before1"},
		logproto.Entry{Timestamp: now.Add(-2 * time.Hour).Add(30 * time.Second), Line: "foo_before2"},
		logproto.Entry{Timestamp: now.Add(-2 * time.Hour).Add(45 * time.Second), Line: "foo_before3"},
	)

	// Data within query range
	builder.addStreamAndFlush(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now, Line: "foo1"},
		logproto.Entry{Timestamp: now.Add(30 * time.Second), Line: "foo2"},
		logproto.Entry{Timestamp: now.Add(45 * time.Second), Line: "foo3"},
		logproto.Entry{Timestamp: now.Add(50 * time.Second), Line: "foo4"},
	)
	builder.addStreamAndFlush(
		`{app="foo", env="dev"}`,
		logproto.Entry{Timestamp: now.Add(10 * time.Second), Line: "foo5"},
		logproto.Entry{Timestamp: now.Add(20 * time.Second), Line: "foo6"},
		logproto.Entry{Timestamp: now.Add(35 * time.Second), Line: "foo7"},
	)

	builder.addStreamAndFlush(
		`{app="bar", env="prod"}`,
		logproto.Entry{Timestamp: now.Add(5 * time.Second), Line: "bar1"},
		logproto.Entry{Timestamp: now.Add(15 * time.Second), Line: "bar2"},
		logproto.Entry{Timestamp: now.Add(25 * time.Second), Line: "bar3"},
		logproto.Entry{Timestamp: now.Add(40 * time.Second), Line: "bar4"},
	)
	builder.addStreamAndFlush(
		`{app="bar", env="dev"}`,
		logproto.Entry{Timestamp: now.Add(8 * time.Second), Line: "bar5"},
		logproto.Entry{Timestamp: now.Add(18 * time.Second), Line: "bar6"},
		logproto.Entry{Timestamp: now.Add(38 * time.Second), Line: "bar7"},
	)

	builder.addStreamAndFlush(
		`{app="baz", env="prod", team="a"}`,
		logproto.Entry{Timestamp: now.Add(12 * time.Second), Line: "baz1"},
		logproto.Entry{Timestamp: now.Add(22 * time.Second), Line: "baz2"},
		logproto.Entry{Timestamp: now.Add(32 * time.Second), Line: "baz3"},
		logproto.Entry{Timestamp: now.Add(42 * time.Second), Line: "baz4"},
	)

	builder.addStreamAndFlush(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now.Add(2 * time.Hour), Line: "foo_after1"},
		logproto.Entry{Timestamp: now.Add(2 * time.Hour).Add(30 * time.Second), Line: "foo_after2"},
		logproto.Entry{Timestamp: now.Add(2 * time.Hour).Add(45 * time.Second), Line: "foo_after3"},
	)

	return now
}
