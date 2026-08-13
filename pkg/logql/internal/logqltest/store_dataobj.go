package logqltest

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier"
)

// newTestingDataObjQuerier materialises the loaded streams into a real data object and a real
// metastore, both on a filesystem bucket, and returns a querier that serves supported metric
// queries from them, delegating everything else to chunkStore.
func newTestingDataObjQuerier(t *testing.T, chunkStore querier.Store, loaded []logproto.Stream) logql.Querier {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), tenant)

	// An eval before any load leaves loaded empty; logsobj.Builder.Flush rejects an empty builder, so
	// fall back to the (also empty) chunk store.
	if len(loaded) == 0 {
		return chunkStore
	}

	bucket, err := filesystem.NewBucket(t.TempDir())
	require.NoError(t, err)
	up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          1 << 20,
		TargetObjectSize:        10 << 20,
		TargetSectionSize:       1 << 20,
		BufferSize:              1 << 20,
		SectionStripeMergeLimit: 2,
	}

	// Build and upload the logs object holding the sample data.
	logsBuilder, err := logsobj.NewBuilder(logsobj.BuilderConfig{BuilderBaseConfig: cfg}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)
	for _, s := range loaded {
		require.NoError(t, logsBuilder.Append(tenant, s, epoch))
	}
	logsObj, logsCloser, err := logsBuilder.Flush()
	require.NoError(t, err)
	logsPath, err := up.Upload(ctx, logsObj)
	require.NoError(t, err)
	require.NoError(t, logsCloser.Close())

	// Index the uploaded logs object the way the production indexer does, then upload the index and
	// register it in the metastore table of contents.
	calc := index.NewCalculator(mustIndexBuilder(t, cfg))
	logsRO, err := dataobj.FromBucket(ctx, bucket, logsPath, 0)
	require.NoError(t, err)
	require.NoError(t, calc.Calculate(ctx, log.NewNopLogger(), logsRO, logsPath))

	idxObj, idxCloser, timeRanges, err := calc.Flush()
	require.NoError(t, err)
	idxPath, err := up.Upload(ctx, idxObj)
	require.NoError(t, err)
	require.NoError(t, idxCloser.Close())

	toc := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	require.NoError(t, toc.WriteEntry(ctx, idxPath, timeRanges))

	ms := metastore.NewObjectMetastore(bucket, metastore.Config{ReadPostingsSections: true}, log.NewNopLogger(), metastore.NewObjectMetastoreMetrics(nil))
	return querier.NewDataObjSampleStore(chunkStore, bucket, ms, log.NewNopLogger(), nil)
}

func mustIndexBuilder(t *testing.T, cfg logsobj.BuilderBaseConfig) *indexobj.Builder {
	t.Helper()
	b, err := indexobj.NewBuilder(cfg, nil)
	require.NoError(t, err)
	return b
}
