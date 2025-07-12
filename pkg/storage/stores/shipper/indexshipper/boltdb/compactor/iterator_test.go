package compactor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
)

func Test_ChunkIterator(t *testing.T) {
	for _, tt := range allSchemas {
		t.Run(tt.schema, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			store := newTestStore(t, cm)
			chunkfmt, headfmt, err := tt.config.ChunkFormat()
			require.NoError(t, err)

			c1 := createChunk(t, chunkfmt, headfmt, "1", labels.FromStrings("foo", "bar"), tt.from, tt.from.Add(1*time.Hour))
			c2 := createChunk(t, chunkfmt, headfmt, "2", labels.FromStrings("foo", "buzz", "bar", "foo"), tt.from, tt.from.Add(1*time.Hour))

			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
				c1, c2,
			}))

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, 1)
			var actual []retention.Chunk
			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				seriesCleaner := newSeriesCleaner(tx.Bucket(local.IndexBucketName), tt.config, tables[0].name)
				return ForEachSeries(context.Background(), tx.Bucket(local.IndexBucketName), tt.config, func(series retention.Series) (err error) {
					actual = append(actual, series.Chunks()...)
					if string(series.UserID()) == c2.UserID {
						return seriesCleaner.RemoveChunk(actual[1].From, actual[1].Through, series.UserID(), series.Labels(), actual[1].ChunkID)
					}
					return nil
				})
			})
			require.NoError(t, err)
			require.Equal(t, []retention.Chunk{
				retentionChunkFromChunk(store.schemaCfg, c1),
				retentionChunkFromChunk(store.schemaCfg, c2),
			}, actual)

			// second pass we delete c2
			actual = actual[:0]
			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				return ForEachSeries(context.Background(), tx.Bucket(local.IndexBucketName), tt.config, func(series retention.Series) (err error) {
					actual = append(actual, series.Chunks()...)
					return nil
				})
			})
			require.NoError(t, err)
			require.Equal(t, []retention.Chunk{
				retentionChunkFromChunk(store.schemaCfg, c1),
			}, actual)
		})
	}
}

func Test_ChunkIteratorContextCancelation(t *testing.T) {
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	store := newTestStore(t, cm)

	from := schemaCfg.Configs[0].From.Time
	chunkfmt, headfmt, err := schemaCfg.Configs[0].ChunkFormat()
	require.NoError(t, err)

	c1 := createChunk(t, chunkfmt, headfmt, "1", labels.FromStrings("foo", "bar"), from, from.Add(1*time.Hour))
	c2 := createChunk(t, chunkfmt, headfmt, "2", labels.FromStrings("foo", "buzz", "bar", "foo"), from, from.Add(1*time.Hour))

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{c1, c2}))
	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var actual []retention.Chunk
	err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
		return ForEachSeries(ctx, tx.Bucket(local.IndexBucketName), schemaCfg.Configs[0], func(series retention.Series) (err error) {
			actual = append(actual, series.Chunks()...)
			cancel()
			return nil
		})
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, actual, 1)
}

func Test_SeriesCleaner(t *testing.T) {
	for _, tt := range allSchemas {
		t.Run(tt.schema, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			store := newTestStore(t, cm)
			chunkfmt, headfmt, err := tt.config.ChunkFormat()
			require.NoError(t, err)

			c1 := createChunk(t, chunkfmt, headfmt, "1", labels.FromStrings("foo", "bar"), tt.from, tt.from.Add(1*time.Hour))
			c2 := createChunk(t, chunkfmt, headfmt, "2", labels.FromStrings("foo", "buzz", "bar", "foo"), tt.from, tt.from.Add(1*time.Hour))
			c3 := createChunk(t, chunkfmt, headfmt, "2", labels.FromStrings("foo", "buzz", "bar", "buzz"), tt.from, tt.from.Add(1*time.Hour))

			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
				c1, c2, c3,
			}))

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, 1)
			// remove c1, c2 chunk
			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				seriesCleaner := newSeriesCleaner(tx.Bucket(local.IndexBucketName), tt.config, tables[0].name)
				return ForEachSeries(context.Background(), tx.Bucket(local.IndexBucketName), tt.config, func(series retention.Series) (err error) {
					if series.Labels().Get("bar") == "foo" {
						for _, chk := range series.Chunks() {
							require.NoError(t, seriesCleaner.RemoveChunk(chk.From, chk.Through, series.UserID(), series.Labels(), chk.ChunkID))
						}
					}
					return nil
				})
			})
			require.NoError(t, err)

			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				cleaner := newSeriesCleaner(tx.Bucket(local.IndexBucketName), tt.config, tables[0].name)
				if err := cleaner.CleanupSeries([]byte(c2.UserID), c2.Metric); err != nil {
					return err
				}

				// remove series for c1 without __name__ label, which should work just fine
				return cleaner.CleanupSeries([]byte(c1.UserID), labels.NewBuilder(c1.Metric).Del(labels.MetricName).Labels())
			})
			require.NoError(t, err)

			err = tables[0].DB.View(func(tx *bbolt.Tx) error {
				return tx.Bucket(local.IndexBucketName).ForEach(func(k, _ []byte) error {
					c1SeriesID := labelsSeriesID(c1.Metric)
					c2SeriesID := labelsSeriesID(c2.Metric)
					series, ok, err := parseLabelIndexSeriesID(decodeKey(k))
					if !ok {
						return nil
					}
					if err != nil {
						return err
					}

					if string(c1SeriesID) == string(series) {
						require.Fail(t, "series for c1 should be deleted", c1SeriesID)
					} else if string(c2SeriesID) == string(series) {
						require.Fail(t, "series for c2 should be deleted", c2SeriesID)
					}

					return nil
				})
			})
			require.NoError(t, err)
		})
	}
}

func labelsSeriesID(ls labels.Labels) []byte {
	h := sha256.Sum256([]byte(labelsString(ls)))
	return encodeBase64Bytes(h[:])
}

func encodeBase64Bytes(bytes []byte) []byte {
	encodedLen := base64.RawStdEncoding.EncodedLen(len(bytes))
	encoded := make([]byte, encodedLen)
	base64.RawStdEncoding.Encode(encoded, bytes)
	return encoded
}

// Backwards-compatible with model.Metric.String()
func labelsString(ls labels.Labels) string {
	metricName := ls.Get(labels.MetricName)
	if metricName != "" && ls.Len() == 1 {
		return metricName
	}
	var b strings.Builder
	b.Grow(1000)

	b.WriteString(metricName)
	b.WriteByte('{')
	i := 0
	ls.Range(func(l labels.Label) {
		if l.Name == labels.MetricName {
			return
		}
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}
		b.WriteString(l.Name)
		b.WriteByte('=')
		var buf [1000]byte
		b.Write(strconv.AppendQuote(buf[:0], l.Value))
		i++
	})
	b.WriteByte('}')

	return b.String()
}

func retentionChunkFromChunk(s config.SchemaConfig, c chunk.Chunk) retention.Chunk {
	return retention.Chunk{
		ChunkID: s.ExternalKey(c.ChunkRef),
		From:    c.From,
		Through: c.Through,
	}
}

func Benchmark_ChunkIterator(b *testing.B) {
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	store := newTestStore(b, cm)
	chunkfmt, headfmt, err := allSchemas[0].config.ChunkFormat()
	require.NoError(b, err)
	for i := 0; i < 100; i++ {
		require.NoError(b, store.Put(context.TODO(),
			[]chunk.Chunk{
				createChunk(b, chunkfmt, headfmt, "1",
					labels.FromStrings("foo", "bar", "i", fmt.Sprintf("%d", i)),
					allSchemas[0].from, allSchemas[0].from.Add(1*time.Hour)),
			},
		))
	}
	store.Stop()
	b.ReportAllocs()
	b.ResetTimer()

	var total int
	_ = store.indexTables()[0].Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(local.IndexBucketName)
		for n := 0; n < b.N; n++ {
			err := ForEachSeries(context.Background(), bucket, allSchemas[0].config, func(series retention.Series) (err error) {
				total += len(series.Chunks())
				return nil
			})
			require.NoError(b, err)
		}
		return errors.New("don't commit")
	})
	b.Logf("Total chunk ref:%d", total)
}
