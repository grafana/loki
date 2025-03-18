package metastore

import (
	"bytes"
	"context"
	"os"
	"slices"
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

const (
	tenantID = "test-tenant"
)

var (
	now = time.Now().UTC()

	// our streams won't use any log lines, therefore leave them out of the Entry structs
	streams = []logproto.Stream{
		{
			Labels:  `{app="foo", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(-2 * time.Hour)}},
		},
		{
			Labels:  `{app="foo", env="dev"}`,
			Entries: []logproto.Entry{{Timestamp: now}},
		},
		{
			Labels:  `{app="bar", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(5 * time.Second)}},
		},
		{
			Labels:  `{app="bar", env="dev"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(8 * time.Minute)}},
		},
		{
			Labels:  `{app="baz", env="prod", team="a"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(12 * time.Minute)}},
		},
		{
			Labels:  `{app="foo", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(-12 * time.Hour)}},
		},
		{
			Labels:  `{app="foo", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(12 * time.Hour)}},
		},
	}
)

// Similar to store_test.go -- we need a populated dataobj/builder/metastore to test labels and values
type testDataBuilder struct {
	t      *testing.T
	bucket objstore.Bucket

	builder  *dataobj.Builder
	meta     *Updater
	uploader *uploader.Uploader
}

func (b *testDataBuilder) addStreamAndFlush(stream logproto.Stream) {
	err := b.builder.Append(stream)
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

func TestLabelsSingleMatcher(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)

		require.Len(t, matchedLabels, 3)
		for _, expectedLabel := range []string{"env", "team", "app"} {
			require.NotEqual(t, slices.Index(matchedLabels, expectedLabel), -1)
		}
	})
}

func TestLabelsEmptyMatcher(t *testing.T) {
	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 3)
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

func TestValuesSingleMatcher(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 5)
	})
}

func TestValuesEmptyMatcher(t *testing.T) {
	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, matchedValues, 6)
		for _, expectedValue := range []string{"foo", "prod", "bar", "dev", "baz", "a"} {
			require.NotEqual(t, slices.Index(matchedValues, expectedValue), -1)
		}
	})
}

func queryMetastore(t *testing.T, tenantID string, mfunc func(context.Context, time.Time, time.Time, Metastore)) {
	now := time.Now().UTC()
	start := now.Add(-time.Hour * 5)
	end := now.Add(time.Hour * 5)

	builder := newTestDataBuilder(t, tenantID)

	for _, stream := range streams {
		builder.addStreamAndFlush(stream)
	}

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
