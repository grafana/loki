package logqltest

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline/builder"
	loglinestore "github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// newTestingLoglineStoreWithStreams builds indexes with the production builder and polls the
// store so the indexes are ready to query, including when streams is empty.
func newTestingLoglineStoreWithStreams(t *testing.T, streams []logproto.Stream) *loglinestore.Store {
	t.Helper()

	bucket := objstore.NewInMemBucket()
	t.Cleanup(func() { require.NoError(t, bucket.Close()) })

	logger := log.NewNopLogger()
	minDate := epoch.Format(time.DateOnly)
	indexStore, err := loglinestore.NewStore(bucket, loglinestore.Config{
		MinDate:              minDate,
		QueryIngestersWithin: 0,
	}, logger, nil)
	require.NoError(t, err)

	cfg := builder.Config{
		PostingsBufferPairs: 1 << 16,
		ScratchDir:          t.TempDir(),
	}
	b, err := builder.NewBuilder(cfg, minDate, logger, nil)
	require.NoError(t, err)
	defer b.Clear()

	recordTime := epoch
	for _, stream := range streams {
		parsedLabels, err := syntax.ParseLabels(stream.Labels)
		require.NoError(t, err)
		require.NoError(t, b.ProcessStream(&stream, &parsedLabels, recordTime, builder.RecordRef{}))
	}

	prepared, err := b.Prepare()
	require.NoError(t, err)
	require.NoError(t, prepared.Upload(t.Context(), indexStore))
	require.NoError(t, indexStore.Poll(t.Context()))
	return indexStore
}
