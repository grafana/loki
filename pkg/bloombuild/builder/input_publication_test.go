package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/iter"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type publicationStore struct {
	fakeBloomStore
	client *publicationClient
}

func (s publicationStore) Client(model.Time) (bloomshipper.Client, error) { return s.client, nil }
func (s publicationStore) BloomMetrics() *v1.Metrics                      { return v1.NewMetrics(nil) }

type publicationClient struct {
	fakeBloomClient
	blocks int
	metas  []bloomshipper.Meta
}

func (c *publicationClient) PutBlock(context.Context, bloomshipper.Block) error {
	c.blocks++
	return nil
}

func (c *publicationClient) PutMeta(_ context.Context, meta bloomshipper.Meta) error {
	c.metas = append(c.metas, meta)
	return nil
}

type publicationChunkLoader struct{ failure error }

func (l *publicationChunkLoader) Load(_ context.Context, _ string, series *v1.Series) *ChunkItersByFingerprint {
	entries := iter.NewStreamIterator(logproto.Stream{Entries: []logproto.Entry{{
		Timestamp: time.Unix(1, 0), Line: "synthetic record",
		StructuredMetadata: []logproto.LabelAdapter{{Name: "request_id", Value: "new-request"}},
	}}})
	// Read a real entry before reporting an error, so failure cannot be mistaken
	// for an empty input or a failure before any work was performed.
	return &ChunkItersByFingerprint{itr: v2.NewSliceIter([]v1.ChunkRefWithIter{{
		Ref: series.Chunks[0], Itr: publicationEntryIterator{EntryIterator: entries, failure: l.failure},
	}})}
}

type publicationEntryIterator struct {
	iter.EntryIterator
	failure error
}

func (i publicationEntryIterator) Err() error { return i.failure }

func TestProcessTaskDoesNotPublishIncompleteInputAndCanRetry(t *testing.T) {
	ctx := context.Background()
	b := setupBuilder(t, "unused", fakeLimits{}, log.NewNopLogger())
	client := &publicationClient{}
	b.bloomStore = publicationStore{client: client}
	loader := &publicationChunkLoader{failure: errors.New("synthetic interrupted read")}
	b.chunkLoader = loader
	b.cfg.WorkingDir = t.TempDir()
	ref := v1.ChunkRef{From: 1, Through: 2, Checksum: 3}
	task := protos.NewTask(plannertest.TestTable, "fake", v1.NewBounds(1, 1), plannertest.TsdbID(1), []protos.Gap{{
		Bounds: v1.NewBounds(1, 1), Series: []*v1.Series{{Fingerprint: 1, Chunks: v1.ChunkRefs{ref}}},
	}})
	created, err := b.processTask(ctx, task)
	require.ErrorIs(t, err, loader.failure)
	require.Empty(t, created)
	require.Zero(t, client.blocks, "failed block must not be uploaded")
	require.Empty(t, client.metas, "failed input must never publish coverage metadata")

	loader.failure = nil
	created, err = b.processTask(ctx, task)
	require.NoError(t, err)
	require.Len(t, created, 1)
	require.Equal(t, 1, client.blocks)
	require.Len(t, client.metas, 1)
	require.Len(t, client.metas[0].Blocks, 1)
}
