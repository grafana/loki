package chunk

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/go-kit/kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	chunkTimeRangeKeyV1a = 1
	chunkTimeRangeKeyV1  = '1'
	chunkTimeRangeKeyV2  = '2'
	chunkTimeRangeKeyV3  = '3'
	chunkTimeRangeKeyV4  = '4'
	chunkTimeRangeKeyV5  = '5'
	metricNameRangeKeyV1 = '6'

	// For v9 schema
	seriesRangeKeyV1      = '7'
	labelSeriesRangeKeyV1 = '8'
	// For v11 schema
	labelNamesRangeKeyV1 = '9'
)

var (
	// ErrNotSupported when a schema doesn't support that particular lookup.
	ErrNotSupported = errors.New("not supported")
	empty           = []byte("-")
)

type hasChunksForIntervalFunc func(userID, seriesID string, from, through model.Time) (bool, error)

// Schema interfaces define methods to calculate the hash and range keys needed
// to write or read chunks from the external index.

// BasicSchema has operation shared between StoreSchema and SeriesStoreSchema
type BaseSchema interface {
	// When doing a read, use these methods to return the list of entries you should query
	GetReadQueriesForMetric(from, through model.Time, userID string, metricName string) ([]IndexQuery, error)
	GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]IndexQuery, error)
	GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]IndexQuery, error)
	FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery
}

// StoreSchema is a schema used by store
type StoreSchema interface {
	BaseSchema

	// When doing a write, use this method to return the list of entries you should write to.
	GetWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error)
}

// SeriesStoreSchema is a schema used by seriesStore
type SeriesStoreSchema interface {
	BaseSchema

	// returns cache key string and []IndexEntry per bucket, matched in order
	GetCacheKeysAndLabelWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]string, [][]IndexEntry, error)
	GetChunkWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error)

	// If the query resulted in series IDs, use this method to find chunks.
	GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error)
	// Returns queries to retrieve all label names of multiple series by id.
	GetLabelNamesForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error)

	// GetSeriesDeleteEntries returns IndexEntry's for deleting SeriesIDs from SeriesStore.
	// Since SeriesIDs are created per bucket, it makes sure that we don't include series entries which are in use by verifying using hasChunksForIntervalFunc i.e
	// It checks first and last buckets covered by the time interval to see if a SeriesID still has chunks in the store,
	// if yes then it doesn't include IndexEntry's for that bucket for deletion.
	GetSeriesDeleteEntries(from, through model.Time, userID string, metric labels.Labels, hasChunksForIntervalFunc hasChunksForIntervalFunc) ([]IndexEntry, error)
}

// IndexQuery describes a query for entries
type IndexQuery struct {
	TableName string
	HashValue string

	// One of RangeValuePrefix or RangeValueStart might be set:
	// - If RangeValuePrefix is not nil, must read all keys with that prefix.
	// - If RangeValueStart is not nil, must read all keys from there onwards.
	// - If neither is set, must read all keys for that row.
	// RangeValueStart should only be used for querying Chunk IDs.
	// If this is going to change then please take care of func isChunksQuery in pkg/chunk/storage/caching_index_client.go which relies on it.
	RangeValuePrefix []byte
	RangeValueStart  []byte

	// Filters for querying
	ValueEqual []byte

	// If the result of this lookup is immutable or not (for caching).
	Immutable bool
}

// IndexEntry describes an entry in the chunk index
type IndexEntry struct {
	TableName string
	HashValue string

	// For writes, RangeValue will always be set.
	RangeValue []byte

	// New for v6 schema, label value is not written as part of the range key.
	Value []byte
}

type schemaBucketsFunc func(from, through model.Time, userID string) []Bucket

// baseSchema implements BaseSchema given a bucketing function and and set of range key callbacks
type baseSchema struct {
	buckets schemaBucketsFunc
	entries baseEntries
}

// storeSchema implements StoreSchema given a bucketing function and and set of range key callbacks
type storeSchema struct {
	baseSchema
	entries storeEntries
}

// seriesStoreSchema implements SeriesStoreSchema given a bucketing function and and set of range key callbacks
type seriesStoreSchema struct {
	baseSchema
	entries seriesStoreEntries
}

func newStoreSchema(buckets schemaBucketsFunc, entries storeEntries) storeSchema {
	return storeSchema{
		baseSchema: baseSchema{buckets: buckets, entries: entries},
		entries:    entries,
	}
}

func newSeriesStoreSchema(buckets schemaBucketsFunc, entries seriesStoreEntries) seriesStoreSchema {
	return seriesStoreSchema{
		baseSchema: baseSchema{buckets: buckets, entries: entries},
		entries:    entries,
	}
}

func (s storeSchema) GetWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	var result []IndexEntry

	for _, bucket := range s.buckets(from, through, userID) {
		entries, err := s.entries.GetWriteEntries(bucket, metricName, labels, chunkID)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

// returns cache key string and []IndexEntry per bucket, matched in order
func (s seriesStoreSchema) GetCacheKeysAndLabelWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]string, [][]IndexEntry, error) {
	var keys []string
	var indexEntries [][]IndexEntry

	for _, bucket := range s.buckets(from, through, userID) {
		key := strings.Join([]string{
			bucket.tableName,
			bucket.hashKey,
			string(labelsSeriesID(labels)),
		},
			"-",
		)
		// This is just encoding to remove invalid characters so that we can put them in memcache.
		// We're not hashing them as the length of the key is well within memcache bounds. tableName + userid + day + 32Byte(seriesID)
		key = hex.EncodeToString([]byte(key))
		keys = append(keys, key)

		entries, err := s.entries.GetLabelWriteEntries(bucket, metricName, labels, chunkID)
		if err != nil {
			return nil, nil, err
		}
		indexEntries = append(indexEntries, entries)
	}
	return keys, indexEntries, nil
}

func (s seriesStoreSchema) GetChunkWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	var result []IndexEntry

	for _, bucket := range s.buckets(from, through, userID) {
		entries, err := s.entries.GetChunkWriteEntries(bucket, metricName, labels, chunkID)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s baseSchema) GetReadQueriesForMetric(from, through model.Time, userID string, metricName string) ([]IndexQuery, error) {
	var result []IndexQuery

	buckets := s.buckets(from, through, userID)
	for _, bucket := range buckets {
		entries, err := s.entries.GetReadMetricQueries(bucket, metricName)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s baseSchema) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]IndexQuery, error) {
	var result []IndexQuery

	buckets := s.buckets(from, through, userID)
	for _, bucket := range buckets {
		entries, err := s.entries.GetReadMetricLabelQueries(bucket, metricName, labelName)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s baseSchema) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	var result []IndexQuery

	buckets := s.buckets(from, through, userID)
	for _, bucket := range buckets {
		entries, err := s.entries.GetReadMetricLabelValueQueries(bucket, metricName, labelName, labelValue)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s seriesStoreSchema) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
	var result []IndexQuery

	buckets := s.buckets(from, through, userID)
	for _, bucket := range buckets {
		entries, err := s.entries.GetChunksForSeries(bucket, seriesID)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

// GetSeriesDeleteEntries returns IndexEntry's for deleting SeriesIDs from SeriesStore.
// Since SeriesIDs are created per bucket, it makes sure that we don't include series entries which are in use by verifying using hasChunksForIntervalFunc i.e
// It checks first and last buckets covered by the time interval to see if a SeriesID still has chunks in the store,
// if yes then it doesn't include IndexEntry's for that bucket for deletion.
func (s seriesStoreSchema) GetSeriesDeleteEntries(from, through model.Time, userID string, metric labels.Labels, hasChunksForIntervalFunc hasChunksForIntervalFunc) ([]IndexEntry, error) {
	metricName := metric.Get(model.MetricNameLabel)
	if metricName == "" {
		return nil, ErrMetricNameLabelMissing
	}

	buckets := s.buckets(from, through, userID)
	if len(buckets) == 0 {
		return nil, nil
	}

	seriesID := string(labelsSeriesID(metric))

	// Only first and last buckets needs to be checked for in-use series ids.
	// Only partially deleted first/last deleted bucket needs to be checked otherwise
	// not since whole bucket is anyways considered for deletion.

	// Bucket times are relative to the bucket i.e for a per-day bucket
	// bucket.from would be the number of milliseconds elapsed since the start of that day.
	// If bucket.from is not 0, it means the from param doesn't align with the start of the bucket.
	if buckets[0].from != 0 {
		bucketStartTime := from - model.Time(buckets[0].from)
		hasChunks, err := hasChunksForIntervalFunc(userID, seriesID, bucketStartTime, bucketStartTime+model.Time(buckets[0].bucketSize)-1)
		if err != nil {
			return nil, err
		}

		if hasChunks {
			buckets = buckets[1:]
			if len(buckets) == 0 {
				return nil, nil
			}
		}
	}

	lastBucket := buckets[len(buckets)-1]

	// Similar to bucket.from, bucket.through here is also relative i.e for a per-day bucket
	// through would be the number of milliseconds elapsed since the start of that day
	// If bucket.through is not equal to max size of bucket, it means the through param doesn't align with the end of the bucket.
	if lastBucket.through != lastBucket.bucketSize {
		bucketStartTime := through - model.Time(lastBucket.through)
		hasChunks, err := hasChunksForIntervalFunc(userID, seriesID, bucketStartTime, bucketStartTime+model.Time(lastBucket.bucketSize)-1)
		if err != nil {
			return nil, err
		}

		if hasChunks {
			buckets = buckets[:len(buckets)-1]
			if len(buckets) == 0 {
				return nil, nil
			}
		}
	}

	var result []IndexEntry

	for _, bucket := range buckets {
		entries, err := s.entries.GetLabelWriteEntries(bucket, metricName, metric, "")
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}

	return result, nil
}

func (s seriesStoreSchema) GetLabelNamesForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
	var result []IndexQuery

	buckets := s.buckets(from, through, userID)
	for _, bucket := range buckets {
		entries, err := s.entries.GetLabelNamesForSeries(bucket, seriesID)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s baseSchema) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery {
	return s.entries.FilterReadQueries(queries, shard)
}

type baseEntries interface {
	GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error)
	GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error)
	GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error)
	FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery
}

// used by storeSchema
type storeEntries interface {
	baseEntries

	GetWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error)
}

// used by seriesStoreSchema
type seriesStoreEntries interface {
	baseEntries

	GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error)
	GetChunkWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error)

	GetChunksForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error)
	GetLabelNamesForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error)
}

// original entries:
// - hash key: <userid>:<bucket>:<metric name>
// - range key: <label name>\0<label value>\0<chunk name>

type originalEntries struct{}

func (originalEntries) GetWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	result := []IndexEntry{}
	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		if strings.ContainsRune(v.Value, '\x00') {
			return nil, fmt.Errorf("label values cannot contain null byte")
		}
		result = append(result, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + metricName,
			RangeValue: rangeValuePrefix([]byte(v.Name), []byte(v.Value), chunkIDBytes),
		})
	}
	return result, nil
}

func (originalEntries) GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + metricName,
			RangeValuePrefix: nil,
		},
	}, nil
}

func (originalEntries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + metricName,
			RangeValuePrefix: rangeValuePrefix([]byte(labelName)),
		},
	}, nil
}

func (originalEntries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	if strings.ContainsRune(labelValue, '\x00') {
		return nil, fmt.Errorf("label values cannot contain null byte")
	}
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + metricName,
			RangeValuePrefix: rangeValuePrefix([]byte(labelName), []byte(labelValue)),
		},
	}, nil
}

func (originalEntries) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery {
	return queries
}

// v3Schema went to base64 encoded label values & a version ID
// - range key: <label name>\0<base64(label value)>\0<chunk name>\0<version 1>

type base64Entries struct {
	originalEntries
}

func (base64Entries) GetWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	result := []IndexEntry{}
	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}

		encodedBytes := encodeBase64Value(v.Value)
		result = append(result, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + metricName,
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV1, []byte(v.Name), encodedBytes, chunkIDBytes),
		})
	}
	return result, nil
}

func (base64Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	encodedBytes := encodeBase64Value(labelValue)
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + metricName,
			RangeValuePrefix: rangeValuePrefix([]byte(labelName), encodedBytes),
		},
	}, nil
}

// v4 schema went to two schemas in one:
// 1) - hash key: <userid>:<hour bucket>:<metric name>:<label name>
//    - range key: \0<base64(label value)>\0<chunk name>\0<version 2>
// 2) - hash key: <userid>:<hour bucket>:<metric name>
//    - range key: \0\0<chunk name>\0<version 3>
type labelNameInHashKeyEntries struct{}

func (labelNameInHashKeyEntries) GetWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	entries := []IndexEntry{
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + metricName,
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV2, nil, nil, chunkIDBytes),
		},
	}

	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		encodedBytes := encodeBase64Value(v.Value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV1, nil, encodedBytes, chunkIDBytes),
		})
	}

	return entries, nil
}

func (labelNameInHashKeyEntries) GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + metricName,
		},
	}, nil
}

func (labelNameInHashKeyEntries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (labelNameInHashKeyEntries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	encodedBytes := encodeBase64Value(labelValue)
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValuePrefix: rangeValuePrefix(nil, encodedBytes),
		},
	}, nil
}

func (labelNameInHashKeyEntries) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery {
	return queries
}

// v5 schema is an extension of v4, with the chunk end time in the
// range key to improve query latency.  However, it did it wrong
// so the chunk end times are ignored.
type v5Entries struct{}

func (v5Entries) GetWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + metricName,
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV3, encodedThroughBytes, nil, chunkIDBytes),
		},
	}

	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		encodedValueBytes := encodeBase64Value(v.Value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV4, encodedThroughBytes, encodedValueBytes, chunkIDBytes),
		})
	}

	return entries, nil
}

func (v5Entries) GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + metricName,
		},
	}, nil
}

func (v5Entries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v5Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, _ string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v5Entries) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery {
	return queries
}

// v6Entries fixes issues with v5 time encoding being wrong (see #337), and
// moves label value out of range key (see #199).
type v6Entries struct{}

func (v6Entries) GetWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + metricName,
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV3, encodedThroughBytes, nil, chunkIDBytes),
		},
	}

	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV5, encodedThroughBytes, nil, chunkIDBytes),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v6Entries) GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + metricName,
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
		},
	}, nil
}

func (v6Entries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
		},
	}, nil
}

func (v6Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
			ValueEqual:      []byte(labelValue),
		},
	}, nil
}

func (v6Entries) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery {
	return queries
}

// v9Entries adds a layer of indirection between labels -> series -> chunks.
type v9Entries struct{}

func (v9Entries) GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	seriesID := labelsSeriesID(labels)

	entries := []IndexEntry{
		// Entry for metricName -> seriesID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + metricName,
			RangeValue: encodeRangeKey(seriesRangeKeyV1, seriesID, nil, nil),
			Value:      empty,
		},
	}

	// Entries for metricName:labelName -> hash(value):seriesID
	// We use a hash of the value to limit its length.
	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		valueHash := sha256bytes(v.Value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v9Entries) GetChunkWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	seriesID := labelsSeriesID(labels)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		// Entry for seriesID -> chunkID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(seriesID),
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV3, encodedThroughBytes, nil, []byte(chunkID)),
		},
	}

	return entries, nil
}

func (v9Entries) GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + metricName,
		},
	}, nil
}

func (v9Entries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v9Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	valueHash := sha256bytes(labelValue)
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValuePrefix: rangeValuePrefix(valueHash),
			ValueEqual:       []byte(labelValue),
		},
	}, nil
}

func (v9Entries) GetChunksForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + string(seriesID),
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
		},
	}, nil
}

func (v9Entries) GetLabelNamesForSeries(_ Bucket, _ []byte) ([]IndexQuery, error) {
	return nil, ErrNotSupported
}

func (v9Entries) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) []IndexQuery {
	return queries
}

// v10Entries builds on v9 by sharding index rows to reduce their size.
type v10Entries struct {
	rowShards uint32
}

func (s v10Entries) GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	seriesID := labelsSeriesID(labels)

	// read first 32 bits of the hash and use this to calculate the shard
	shard := binary.BigEndian.Uint32(seriesID) % s.rowShards

	entries := []IndexEntry{
		// Entry for metricName -> seriesID
		{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s", shard, bucket.hashKey, metricName),
			RangeValue: encodeRangeKey(seriesRangeKeyV1, seriesID, nil, nil),
			Value:      empty,
		},
	}

	// Entries for metricName:labelName -> hash(value):seriesID
	// We use a hash of the value to limit its length.
	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		valueHash := sha256bytes(v.Value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v10Entries) GetChunkWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	seriesID := labelsSeriesID(labels)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		// Entry for seriesID -> chunkID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(seriesID),
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV3, encodedThroughBytes, nil, []byte(chunkID)),
			Value:      empty,
		},
	}

	return entries, nil
}

func (s v10Entries) GetReadMetricQueries(bucket Bucket, metricName string) ([]IndexQuery, error) {
	result := make([]IndexQuery, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, IndexQuery{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%02d:%s:%s", i, bucket.hashKey, metricName),
		})
	}
	return result, nil
}

func (s v10Entries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]IndexQuery, error) {
	result := make([]IndexQuery, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, IndexQuery{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%02d:%s:%s:%s", i, bucket.hashKey, metricName, labelName),
		})
	}
	return result, nil
}

func (s v10Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	valueHash := sha256bytes(labelValue)
	result := make([]IndexQuery, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, IndexQuery{
			TableName:        bucket.tableName,
			HashValue:        fmt.Sprintf("%02d:%s:%s:%s", i, bucket.hashKey, metricName, labelName),
			RangeValuePrefix: rangeValuePrefix(valueHash),
			ValueEqual:       []byte(labelValue),
		})
	}
	return result, nil
}

func (v10Entries) GetChunksForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + string(seriesID),
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
		},
	}, nil
}

func (v10Entries) GetLabelNamesForSeries(_ Bucket, _ []byte) ([]IndexQuery, error) {
	return nil, ErrNotSupported
}

// FilterReadQueries will return only queries that match a certain shard
func (v10Entries) FilterReadQueries(queries []IndexQuery, shard *astmapper.ShardAnnotation) (matches []IndexQuery) {
	if shard == nil {
		return queries
	}

	for _, query := range queries {
		s := strings.Split(query.HashValue, ":")[0]
		n, err := strconv.Atoi(s)
		if err != nil {
			level.Error(util_log.Logger).Log(
				"msg",
				"Unable to determine shard from IndexQuery",
				"HashValue",
				query.HashValue,
				"schema",
				"v10",
			)
		}

		if err == nil && n == shard.Shard {
			matches = append(matches, query)
		}
	}
	return matches
}

// v11Entries builds on v10 but adds index entries for each series to store respective labels.
type v11Entries struct {
	v10Entries
}

func (s v11Entries) GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]IndexEntry, error) {
	seriesID := labelsSeriesID(labels)

	// read first 32 bits of the hash and use this to calculate the shard
	shard := binary.BigEndian.Uint32(seriesID) % s.rowShards

	labelNames := make([]string, 0, len(labels))
	for _, l := range labels {
		if l.Name == model.MetricNameLabel {
			continue
		}
		labelNames = append(labelNames, l.Name)
	}
	data, err := jsoniter.ConfigFastest.Marshal(labelNames)
	if err != nil {
		return nil, err
	}
	entries := []IndexEntry{
		// Entry for metricName -> seriesID
		{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s", shard, bucket.hashKey, metricName),
			RangeValue: encodeRangeKey(seriesRangeKeyV1, seriesID, nil, nil),
			Value:      empty,
		},
		// Entry for seriesID -> label names
		{
			TableName:  bucket.tableName,
			HashValue:  string(seriesID),
			RangeValue: encodeRangeKey(labelNamesRangeKeyV1, nil, nil, nil),
			Value:      data,
		},
	}

	// Entries for metricName:labelName -> hash(value):seriesID
	// We use a hash of the value to limit its length.
	for _, v := range labels {
		if v.Name == model.MetricNameLabel {
			continue
		}
		valueHash := sha256bytes(v.Value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v11Entries) GetLabelNamesForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: string(seriesID),
		},
	}, nil
}
