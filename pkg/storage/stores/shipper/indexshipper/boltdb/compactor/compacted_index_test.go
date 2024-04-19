package compactor

import (
	"bytes"
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/boltdb"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestCompactedIndex_IndexProcessor(t *testing.T) {
	for _, tt := range allSchemas {
		tt := tt
		t.Run(tt.schema, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			testSchema := config.SchemaConfig{Configs: []config.PeriodConfig{tt.config}}
			store := newTestStore(t, cm)
			chunkfmt, headfmt, err := tt.config.ChunkFormat()
			require.NoError(t, err)
			c1 := createChunk(t, chunkfmt, headfmt, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, tt.from, tt.from.Add(1*time.Hour))
			c2 := createChunk(t, chunkfmt, headfmt, "2", labels.Labels{labels.Label{Name: "foo", Value: "bar"}, labels.Label{Name: "fizz", Value: "buzz"}}, tt.from, tt.from.Add(1*time.Hour))
			c3 := createChunk(t, chunkfmt, headfmt, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "buzz"}}, tt.from, tt.from.Add(1*time.Hour))

			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
				c1, c2, c3,
			}))

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, 1)

			compactedIndex := newCompactedIndex(tables[0].DB, tables[0].name, t.TempDir(), tt.config, util_log.Logger)

			// remove c1, c2 chunk and index c4 with same labels as c2
			c4 := createChunk(t, chunkfmt, headfmt, "2", labels.Labels{labels.Label{Name: "foo", Value: "bar"}, labels.Label{Name: "fizz", Value: "buzz"}}, tt.from, tt.from.Add(30*time.Minute))
			err = compactedIndex.ForEachChunk(context.Background(), func(entry retention.ChunkEntry) (deleteChunk bool, err error) {
				if entry.Labels.Get("fizz") == "buzz" {
					chunkIndexed, err := compactedIndex.IndexChunk(c4)
					require.NoError(t, err)
					require.True(t, chunkIndexed)
				}
				return entry.Labels.Get("foo") == "bar", nil
			})
			require.NoError(t, err)

			// remove series for c1 since all its chunks are deleted
			err = compactedIndex.CleanupSeries(entryFromChunk(testSchema, c1).UserID, c1.Metric)
			require.NoError(t, err)

			indexFile, err := compactedIndex.ToIndexFile()
			require.NoError(t, err)

			defer func() {
				path := indexFile.Path()
				require.NoError(t, indexFile.Close())
				require.NoError(t, os.Remove(path))
			}()

			modifiedBoltDB := indexFile.(*boltdb.IndexFile).GetBoltDB()

			err = modifiedBoltDB.View(func(tx *bbolt.Tx) error {
				return tx.Bucket(local.IndexBucketName).ForEach(func(k, _ []byte) error {
					c1SeriesID := entryFromChunk(testSchema, c1).SeriesID
					series, ok, err := parseLabelIndexSeriesID(decodeKey(k))
					if !ok {
						return nil
					}
					if err != nil {
						return err
					}

					if string(c1SeriesID) == string(series) {
						require.Fail(t, "series for c1 should be deleted", c1SeriesID)
					}

					return nil
				})
			})
			require.NoError(t, err)

			expectedChunkEntries := []retention.ChunkEntry{
				entryFromChunk(testSchema, c3),
				entryFromChunk(testSchema, c4),
			}
			chunkEntriesFound := []retention.ChunkEntry{}
			err = modifiedBoltDB.View(func(tx *bbolt.Tx) error {
				return ForEachChunk(context.Background(), tx.Bucket(local.IndexBucketName), tt.config, func(entry retention.ChunkEntry) (deleteChunk bool, err error) {
					chunkEntriesFound = append(chunkEntriesFound, entry)
					return false, nil
				})
			})
			require.NoError(t, err)

			sort.Slice(expectedChunkEntries, func(i, j int) bool {
				return bytes.Compare(expectedChunkEntries[i].ChunkID, expectedChunkEntries[j].ChunkID) < 0
			})

			sort.Slice(chunkEntriesFound, func(i, j int) bool {
				return bytes.Compare(chunkEntriesFound[i].ChunkID, chunkEntriesFound[j].ChunkID) < 0
			})

			require.Equal(t, expectedChunkEntries, chunkEntriesFound)
		})
	}
}
