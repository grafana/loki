package compactor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	cortex_storage "github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/math"
	"github.com/grafana/loki/pkg/util/validation"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const (
	chunkTimeRangeKeyV3 = '3'
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
					}
					return nil
				})
			}))
	}
}

func findSeriesIDsForRules(db *bbolt.DB, config chunk.PeriodConfig, rules []StreamRule) ([][]string, error) {
	schema, err := config.CreateSchema()
	if err != nil {
		return nil, err
	}
	// cover the whole table.
	from, through := config.From.Time, config.From.Time.Add(config.IndexTables.Period)
	result := make([][]string, len(rules))

	for ruleIndex, rule := range rules {
		incomingIDs := make(chan []string)
		incomingErrors := make(chan error)

		for _, matcher := range rule.Matchers {
			go func(matcher *labels.Matcher) {
				ids, err := lookupSeriesByMatcher(db, schema, from, through, rule.UserID, matcher)
				if err != nil {
					incomingErrors <- err
					return
				}
				incomingIDs <- ids
			}(&matcher)
		}
		// intersect. and add to result.
		var ids []string
		var lastErr error
		var initialized bool
		for i := 0; i < len(rule.Matchers); i++ {
			select {
			case incoming := <-incomingIDs:
				if !initialized {
					ids = incoming
					initialized = true
				} else {
					ids = intersectStrings(ids, incoming)
				}
			case err := <-incomingErrors:
				lastErr = err
			}
		}
		if lastErr != nil {
			return nil, err
		}
		result[ruleIndex] = ids
	}

	return result, nil
}

var QueryParallelism = 100

func lookupSeriesByMatcher(
	db *bbolt.DB,
	schema chunk.BaseSchema,
	from, through model.Time,
	userID string,
	matcher *labels.Matcher) ([]string, error) {
	queries, err := schema.GetReadQueriesForMetricLabelValue(
		from, through, userID, "logs", matcher.Name, matcher.Value)
	if err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return nil, nil
	}
	if len(queries) == 1 {
		return lookupSeriesByQuery(db, queries[0])
	}
	queue := make(chan chunk.IndexQuery)
	incomingResult := make(chan struct {
		ids []string
		err error
	})
	n := math.Min(len(queries), QueryParallelism)
	for i := 0; i < n; i++ {
		go func() {
			for {
				query, ok := <-queue
				if !ok {
					return
				}
				res, err := lookupSeriesByQuery(db, query)
				incomingResult <- struct {
					ids []string
					err error
				}{res, err}
			}
		}()
	}
	go func() {
		for _, query := range queries {
			queue <- query
		}
		close(queue)
	}()

	// Now receive all the results.
	var ids []string
	var lastErr error
	for i := 0; i < len(queries); i++ {
		res := <-incomingResult
		if res.err != nil {
			lastErr = res.err
			continue
		}
		ids = append(ids, res.ids...)
	}
	sort.Strings(ids)
	ids = uniqueStrings(ids)
	return ids, lastErr
}

func uniqueStrings(cs []string) []string {
	if len(cs) == 0 {
		return []string{}
	}

	result := make([]string, 1, len(cs))
	result[0] = cs[0]
	i, j := 0, 1
	for j < len(cs) {
		if result[i] == cs[j] {
			j++
			continue
		}
		result = append(result, cs[j])
		i++
		j++
	}
	return result
}

func intersectStrings(left, right []string) []string {
	var (
		i, j   = 0, 0
		result = []string{}
	)
	for i < len(left) && j < len(right) {
		if left[i] == right[j] {
			result = append(result, left[i])
		}

		if left[i] < right[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

const separator = "\000"

func lookupSeriesByQuery(db *bbolt.DB, query chunk.IndexQuery) ([]string, error) {
	start := []byte(query.HashValue + separator + string(query.RangeValueStart))
	rowPrefix := []byte(query.HashValue + separator)
	var res []string
	var components [][]byte
	err := db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		if bucket == nil {
			return nil
		}
		c := bucket.Cursor()
		for k, v := c.Seek(start); k != nil; k, v = c.Next() {
			// technically we can run regex that are not matching empty.
			if len(query.ValueEqual) > 0 && !bytes.Equal(v, query.ValueEqual) {
				continue
			}
			if !bytes.HasPrefix(k, rowPrefix) {
				break
			}
			// parse series ID and add to res
			_, r := decodeKey(k)

			components = decodeRangeKey(r, components)
			if len(components) != 4 {
				continue
			}
			// we store in label entries range keys: label hash value | seriesID | empty | type.
			// and we want the seriesID
			res = append(res, string(components[len(components)-3]))
		}
		return nil
	})

	return res, err
}

func schemaPeriodForTable(config storage.SchemaConfig, tableName string) (chunk.PeriodConfig, bool) {
	for _, schema := range config.Configs {
		periodIndex, err := strconv.ParseInt(strings.TrimPrefix(tableName, schema.IndexTables.Prefix), 10, 64)
		if err != nil {
			continue
		}
		periodSecs := int64((schema.IndexTables.Period) / time.Second)
		if periodIndex == schema.From.Time.Unix()/periodSecs {
			return schema, true
		}
	}
	return chunk.PeriodConfig{}, false
}

type ChunkRef struct {
	UserID   []byte
	SeriesID []byte
	// Fingerprint model.Fingerprint
	From    model.Time
	Through model.Time
}

func (c ChunkRef) String() string {
	return fmt.Sprintf("UserID: %s , SeriesID: %s , Time: [%s,%s]", c.UserID, c.SeriesID, c.From, c.Through)
}

var ErrInvalidIndexKey = errors.New("invalid index key")

type InvalidIndexKeyError struct {
	HashKey  string
	RangeKey string
}

func newInvalidIndexKeyError(h, r []byte) InvalidIndexKeyError {
	return InvalidIndexKeyError{
		HashKey:  string(h),
		RangeKey: string(r),
	}
}

func (e InvalidIndexKeyError) Error() string {
	return fmt.Sprintf("%s: hash_key:%s range_key:%s", ErrInvalidIndexKey, e.HashKey, e.RangeKey)
}

func (e InvalidIndexKeyError) Is(target error) bool {
	return target == ErrInvalidIndexKey
}

func parseChunkRef(hashKey, rangeKey []byte) (ChunkRef, bool, error) {
	// todo reuse memory
	var components [][]byte
	components = decodeRangeKey(rangeKey, components)
	if len(components) == 0 {
		return ChunkRef{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}

	keyType := components[len(components)-1]
	if len(keyType) == 0 || keyType[0] != chunkTimeRangeKeyV3 {
		return ChunkRef{}, false, nil
	}
	chunkID := components[len(components)-2]

	// todo split manually
	parts := bytes.Split(chunkID, []byte("/"))
	if len(parts) != 2 {
		return ChunkRef{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}
	userID := parts[0]
	// todo split manually
	hexParts := bytes.Split(parts[1], []byte(":"))
	if len(hexParts) != 4 {
		return ChunkRef{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}

	from, err := strconv.ParseInt(unsafeGetString(hexParts[1]), 16, 64)
	if err != nil {
		return ChunkRef{}, false, err
	}
	through, err := strconv.ParseInt(unsafeGetString(hexParts[2]), 16, 64)
	if err != nil {
		return ChunkRef{}, false, err
	}

	return ChunkRef{
		UserID:   userID,
		SeriesID: seriesFromHash(hashKey),
		From:     model.Time(from),
		Through:  model.Time(through),
	}, true, nil
}

func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}

func seriesFromHash(h []byte) (seriesID []byte) {
	var index int
	for i := range h {
		if h[i] == ':' {
			index++
		}
		if index == 2 {
			seriesID = h[i+1:]
			return
		}
	}
	return
}

func decodeKey(k []byte) (hashValue, rangeValue []byte) {
	// hashValue + 0 + string(rangeValue)
	for i := range k {
		if k[i] == 0 {
			hashValue = k[:i]
			rangeValue = k[i+1:]
			return
		}
	}
	return
}

func decodeRangeKey(value []byte, components [][]byte) [][]byte {
	components = components[:0]
	i, j := 0, 0
	for j < len(value) {
		if value[j] != 0 {
			j++
			continue
		}
		components = append(components, value[i:j])
		j++
		i = j
	}
	return components
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

type StreamRule struct {
	Matchers []labels.Matcher
	Duration time.Duration
	UserID   string
}

type Rules interface {
	PerTenant(userID string) time.Duration
	PerStream() []StreamRule
}

type fakeRule struct {
	streams []StreamRule
	tenants map[string]time.Duration
}

func (f fakeRule) PerTenant(userID string) time.Duration {
	return f.tenants[userID]
}

func (f fakeRule) PerStream() []StreamRule {
	return f.streams
}
