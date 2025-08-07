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

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Similar to store_test.go -- we need a populated dataobj/builder/metastore to test labels and values
type testMultiTenantDataBuilder struct {
	t      *testing.T
	bucket objstore.Bucket

	builder  *logsobj.MultiTenantBuilder
	meta     *MultiTenantUpdater
	uploader *uploader.MultiTenantUploader
}

func (b *testMultiTenantDataBuilder) addStreamAndFlush(tenantID string, stream logproto.Stream) {
	err := b.builder.Append(tenantID, stream)
	require.NoError(b.t, err)

	buf := bytes.NewBuffer(make([]byte, 0, 1024*1024))
	stats, err := b.builder.Flush(buf)
	require.NoError(b.t, err)

	path, err := b.uploader.Upload(context.Background(), buf)
	require.NoError(b.t, err)

	err = b.meta.Update(context.Background(), []string{tenantID}, path, stats.MinTimestamp, stats.MaxTimestamp)
	require.NoError(b.t, err)

	b.builder.Reset()
}

func TestMultiTenantStreamIDs(t *testing.T) {
	t.Run("not matching streams", func(t *testing.T) {
		queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
			}
			paths, streamIDs, sections, err := mstore.StreamIDs(ctx, start, end, matchers...)
			require.NoError(t, err)
			require.Len(t, paths, 0)
			require.Len(t, streamIDs, 0)
			require.Len(t, sections, 0)
		})
	})

	t.Run("matching streams", func(t *testing.T) {
		queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
				labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
			}
			paths, streamIDs, sections, err := mstore.StreamIDs(ctx, start, end, matchers...)
			require.NoError(t, err)
			require.Len(t, paths, 1)
			require.Len(t, streamIDs, 1)
			require.Len(t, sections, 1)
			require.Equal(t, []int64{1}, streamIDs[0])
			require.Equal(t, 0, sections[0])
		})
	})
}

func TestMultiTenantLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, len(matchers))
	})
}

func TestMultiTenantNonExistentLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "invalid"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 0)
	})
}

func TestMultiTenantMixedLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "invalid"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 0)
	})
}

func TestMultiTenantLabelsSingleMatcher(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)

		require.Len(t, matchedLabels, 3)
		for _, expectedLabel := range []string{"env", "team", "app"} {
			require.NotEqual(t, slices.Index(matchedLabels, expectedLabel), -1)
		}
	})
}

func TestMultiTenantLabelsEmptyMatcher(t *testing.T) {
	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 3)
	})
}

func TestMultiTenantValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, len(matchers))
	})
}

func TestMultiTenantNonExistentValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "invalid"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 0)
	})
}

func TestMultiTenantMixedValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 0)
	})
}

func TestMultiTenantValuesSingleMatcher(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 5)
	})
}

func TestMultiTenantValuesEmptyMatcher(t *testing.T) {
	queryMultienantMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, matchedValues, 6)
		for _, expectedValue := range []string{"foo", "prod", "bar", "dev", "baz", "a"} {
			require.NotEqual(t, slices.Index(matchedValues, expectedValue), -1)
		}
	})
}

func queryMultienantMetastore(t *testing.T, tenantID string, mfunc func(context.Context, time.Time, time.Time, Metastore)) {
	now := time.Now().UTC()
	start := now.Add(-time.Hour * 5)
	end := now.Add(time.Hour * 5)

	builder := newMultiTenantTestDataBuilder(t)

	for _, stream := range testStreams {
		builder.addStreamAndFlush(tenantID, stream)
	}

	mstore := NewMultiTenantObjectMetastore(builder.bucket, log.NewNopLogger(), nil)
	defer func() {
		require.NoError(t, mstore.bucket.Close())
	}()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	mfunc(ctx, start, end, mstore)
}

func newMultiTenantTestDataBuilder(t *testing.T) *testMultiTenantDataBuilder {
	bucket := objstore.NewInMemBucket()

	builder, err := logsobj.NewMultiTenantBuilder(logsobj.BuilderConfig{
		TargetPageSize:          1024 * 1024,      // 1MB
		TargetObjectSize:        10 * 1024 * 1024, // 10MB
		TargetSectionSize:       1024 * 1024,      // 1MB
		BufferSize:              1024 * 1024,      // 1MB
		SectionStripeMergeLimit: 2,
	})
	require.NoError(t, err)

	logger := log.NewLogfmtLogger(os.Stdout)
	logger = log.With(logger, "test", t.Name())

	meta := NewMultiTenantUpdater(UpdaterConfig{}, bucket, logger)
	require.NoError(t, meta.RegisterMetrics(prometheus.NewPedanticRegistry()))

	uploader := uploader.NewMultiTenant(uploader.Config{SHAPrefixSize: 2}, bucket, logger)
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	return &testMultiTenantDataBuilder{
		t:        t,
		bucket:   bucket,
		builder:  builder,
		meta:     meta,
		uploader: uploader,
	}
}
