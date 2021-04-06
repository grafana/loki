package retention

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	cortex_storage "github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/util/validation"
)

func Test_Retention(t *testing.T) {
	indexDir, err := ioutil.TempDir("", "boltdb_test")
	require.Nil(t, err)

	chunkDir, err := ioutil.TempDir("", "chunk_test")
	require.Nil(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(indexDir))
		require.NoError(t, os.RemoveAll(chunkDir))
	}()
	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	schemaCfg := storage.SchemaConfig{
		SchemaConfig: chunk.SchemaConfig{
			Configs: []chunk.PeriodConfig{
				{
					From:       chunk.DayTime{Time: model.Earliest},
					IndexType:  "boltdb",
					ObjectType: "filesystem",
					Schema:     "v9",
					IndexTables: chunk.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					},
					RowShards: 16,
				},
				{
					From:       chunk.DayTime{Time: model.Earliest.Add(25 * time.Hour)},
					IndexType:  "boltdb",
					ObjectType: "filesystem",
					Schema:     "v11",
					IndexTables: chunk.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					},
					RowShards: 16,
				},
			},
		},
	}
	require.NoError(t, schemaCfg.SchemaConfig.Validate())

	config := storage.Config{
		Config: cortex_storage.Config{
			BoltDBConfig: local.BoltDBConfig{
				Directory: indexDir,
			},
			FSConfig: local.FSConfig{
				Directory: chunkDir,
			},
		},
	}
	chunkStore, err := cortex_storage.NewStore(
		config.Config,
		chunk.StoreConfig{},
		schemaCfg.SchemaConfig,
		limits,
		nil,
		nil,
		util_log.Logger,
	)
	require.NoError(t, err)

	store, err := storage.NewStore(config, schemaCfg, chunkStore, nil)
	require.NoError(t, err)

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, model.Earliest, model.Earliest.Add(1*time.Hour)),
		createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}}, model.Earliest.Add(26*time.Hour), model.Earliest.Add(27*time.Hour)),
	}))

	store.Stop()

	indexFilesInfo, err := ioutil.ReadDir(indexDir)
	require.NoError(t, err)

	retentionRules := fakeRule{
		streams: []StreamRule{
			{
				UserID: "1",
				Matchers: []labels.Matcher{
					{
						Type:  labels.MatchEqual,
						Name:  "foo",
						Value: "bar",
					},
				},
			},
		},
	}

	// 1-  Get all series ID for given retention per stream....
	// 2 - Delete from index and Mark for delete all chunk  based on retention with seriesID/tenantID.
	// 3 - Seek chunk entries via series id for each series and verify if we still have chunk.
	// 4 - Delete Label entries for empty series with empty chunk entries.

	// For 1. only equality matcher are OK so we can use GetReadMetricLabelValueQueries
	for _, indexFileInfo := range indexFilesInfo {
		db, err := shipper_util.SafeOpenBoltdbFile(filepath.Join(indexDir, indexFileInfo.Name()))
		fmt.Fprintf(os.Stdout, "Opening Table %s\n", indexFileInfo.Name())
		require.NoError(t, err)

		// 1 - Get all series ID for given retention per stream....

		// 1.1 find the schema for this table

		currentSchema, ok := schemaPeriodForTable(schemaCfg, indexFileInfo.Name())
		if !ok {
			fmt.Fprintf(os.Stdout, "Could not find Schema for Table %s\n", indexFileInfo.Name())
			continue
		}
		fmt.Fprintf(os.Stdout, "Found Schema for Table %s => %+v\n", indexFileInfo.Name(), currentSchema)
		ids, err := findSeriesIDsForRules(db, currentSchema, retentionRules.PerStream())
		require.NoError(t, err)
		fmt.Fprintf(os.Stdout, "Found IDS for rules %+v\n", ids)
		require.NoError(t,
			db.Update(func(tx *bbolt.Tx) error {
				return tx.Bucket(bucketName).ForEach(func(k, v []byte) error {
					ref, ok, err := parseChunkRef(decodeKey(k))
					if err != nil {
						return err
					}
					if ok {
						fmt.Fprintf(os.Stdout, "%+v\n", ref)
						return nil
					}
					_, r := decodeKey(k)
					components := decodeRangeKey(r, nil)
					keyType := components[len(components)-1]

					fmt.Fprintf(os.Stdout, "type:%s \n", keyType)
					return nil
				})
			}))
	}
}

func createChunk(t testing.TB, userID string, lbs labels.Labels, from model.Time, through model.Time) chunk.Chunk {
	t.Helper()
	const (
		targetSize = 1500 * 1024
		blockSize  = 256 * 1024
	)
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels()
	fp := client.Fingerprint(lbs)
	chunkEnc := chunkenc.NewMemChunk(chunkenc.EncSnappy, blockSize, targetSize)

	for ts := from; ts.Before(through); ts = ts.Add(1 * time.Minute) {
		require.NoError(t, chunkEnc.Append(&logproto.Entry{
			Timestamp: ts.Time(),
			Line:      ts.String(),
		}))
	}
	c := chunk.NewChunk(userID, fp, metric, chunkenc.NewFacade(chunkEnc, blockSize, targetSize), from, through)
	require.NoError(t, c.Encode())
	return c
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
	if metricName != "" && len(ls) == 1 {
		return metricName
	}
	var b strings.Builder
	b.Grow(1000)

	b.WriteString(metricName)
	b.WriteByte('{')
	i := 0
	for _, l := range ls {
		if l.Name == labels.MetricName {
			continue
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
	}
	b.WriteByte('}')

	return b.String()
}

func Test_AllBucketHash(t *testing.T) {
	fmt.Fprintf(os.Stdout, "BucketHash %+v", allBucketsHashes(chunk.PeriodConfig{
		From:       chunk.DayTime{Time: model.Now()},
		IndexType:  "boltdb",
		ObjectType: "filesystem",
		Schema:     "v9",
		IndexTables: chunk.PeriodicTableConfig{
			Prefix: "index_",
			Period: time.Hour * 24,
		},
		RowShards: 16,
	}, "foo"))

	fmt.Fprintf(os.Stdout, "BucketHash %+v", bucketsHashes(model.Now().Add(-2*time.Hour), model.Now().Add(48*time.Hour), chunk.PeriodConfig{
		From:       chunk.DayTime{Time: model.Now()},
		IndexType:  "boltdb",
		ObjectType: "filesystem",
		Schema:     "v9",
		IndexTables: chunk.PeriodicTableConfig{
			Prefix: "index_",
			Period: time.Hour * 24,
		},
		RowShards: 16,
	}, "foo"))
}
