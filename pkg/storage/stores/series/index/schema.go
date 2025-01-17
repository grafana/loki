package index

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
	ErrNotSupported           = errors.New("not supported")
	ErrMetricNameLabelMissing = errors.New("metric name label missing")
	empty                     = []byte("-")
)

type hasChunksForIntervalFunc func(userID, seriesID string, from, through model.Time) (bool, error)

// Schema interfaces define methods to calculate the hash and range keys needed
// to write or read chunks from the external index.

// SeriesStoreSchema is a schema used by seriesStore
type SeriesStoreSchema interface {
	// When doing a read, use these methods to return the list of entries you should query
	GetReadQueriesForMetric(from, through model.Time, userID string, metricName string) ([]Query, error)
	GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]Query, error)
	GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]Query, error)
	FilterReadQueries(queries []Query, shard *astmapper.ShardAnnotation) []Query

	// returns cache key string and []IndexEntry per bucket, matched in order
	GetCacheKeysAndLabelWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]string, [][]Entry, error)
	GetChunkWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]Entry, error)

	// If the query resulted in series IDs, use this method to find chunks.
	GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]Query, error)
	// Returns queries to retrieve all label names of multiple series by id.
	GetLabelNamesForSeries(from, through model.Time, userID string, seriesID []byte) ([]Query, error)
}

type schemaBucketsFunc func(from, through model.Time, userID string) []Bucket

// baseSchema implements BaseSchema given a bucketing function and and set of range key callbacks
type baseSchema struct {
	buckets schemaBucketsFunc
	entries baseEntries
}

// seriesStoreSchema implements SeriesStoreSchema given a bucketing function and and set of range key callbacks
type seriesStoreSchema struct {
	baseSchema
	entries seriesStoreEntries
}

func newSeriesStoreSchema(buckets schemaBucketsFunc, entries seriesStoreEntries) seriesStoreSchema {
	return seriesStoreSchema{
		baseSchema: baseSchema{buckets: buckets, entries: entries},
		entries:    entries,
	}
}

// returns cache key string and []IndexEntry per bucket, matched in order
func (s seriesStoreSchema) GetCacheKeysAndLabelWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]string, [][]Entry, error) {
	var keys []string
	var indexEntries [][]Entry

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

func (s seriesStoreSchema) GetChunkWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]Entry, error) {
	var result []Entry

	for _, bucket := range s.buckets(from, through, userID) {
		entries, err := s.entries.GetChunkWriteEntries(bucket, metricName, labels, chunkID)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s baseSchema) GetReadQueriesForMetric(from, through model.Time, userID string, metricName string) ([]Query, error) {
	var result []Query

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

func (s baseSchema) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]Query, error) {
	var result []Query

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

func (s baseSchema) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]Query, error) {
	var result []Query

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

func (s seriesStoreSchema) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]Query, error) {
	var result []Query

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
func (s seriesStoreSchema) GetSeriesDeleteEntries(from, through model.Time, userID string, metric labels.Labels, hasChunksForIntervalFunc hasChunksForIntervalFunc) ([]Entry, error) {
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

	var result []Entry

	for _, bucket := range buckets {
		entries, err := s.entries.GetLabelWriteEntries(bucket, metricName, metric, "")
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}

	return result, nil
}

func (s seriesStoreSchema) GetLabelNamesForSeries(from, through model.Time, userID string, seriesID []byte) ([]Query, error) {
	var result []Query

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

func (s baseSchema) FilterReadQueries(queries []Query, shard *astmapper.ShardAnnotation) []Query {
	return s.entries.FilterReadQueries(queries, shard)
}

type baseEntries interface {
	GetReadMetricQueries(bucket Bucket, metricName string) ([]Query, error)
	GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]Query, error)
	GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]Query, error)
	FilterReadQueries(queries []Query, shard *astmapper.ShardAnnotation) []Query
}

// used by seriesStoreSchema
type seriesStoreEntries interface {
	baseEntries

	GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]Entry, error)
	GetChunkWriteEntries(bucket Bucket, metricName string, labels labels.Labels, chunkID string) ([]Entry, error)

	GetChunksForSeries(bucket Bucket, seriesID []byte) ([]Query, error)
	GetLabelNamesForSeries(bucket Bucket, seriesID []byte) ([]Query, error)
}

// v9Entries adds a layer of indirection between labels -> series -> chunks.
type v9Entries struct{}

func (v9Entries) GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, _ string) ([]Entry, error) {
	seriesID := labelsSeriesID(labels)

	entries := []Entry{
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
		entries = append(entries, Entry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v9Entries) GetChunkWriteEntries(bucket Bucket, _ string, labels labels.Labels, chunkID string) ([]Entry, error) {
	seriesID := labelsSeriesID(labels)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []Entry{
		// Entry for seriesID -> chunkID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(seriesID),
			RangeValue: encodeRangeKey(chunkTimeRangeKeyV3, encodedThroughBytes, nil, []byte(chunkID)),
		},
	}

	return entries, nil
}

func (v9Entries) GetReadMetricQueries(bucket Bucket, metricName string) ([]Query, error) {
	return []Query{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + metricName,
		},
	}, nil
}

func (v9Entries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]Query, error) {
	return []Query{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v9Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]Query, error) {
	valueHash := sha256bytes(labelValue)
	return []Query{
		{
			TableName:        bucket.tableName,
			HashValue:        fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValuePrefix: rangeValuePrefix(valueHash),
			ValueEqual:       []byte(labelValue),
		},
	}, nil
}

func (v9Entries) GetChunksForSeries(bucket Bucket, seriesID []byte) ([]Query, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []Query{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + string(seriesID),
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
		},
	}, nil
}

func (v9Entries) GetLabelNamesForSeries(_ Bucket, _ []byte) ([]Query, error) {
	return nil, ErrNotSupported
}

func (v9Entries) FilterReadQueries(queries []Query, _ *astmapper.ShardAnnotation) []Query {
	return queries
}

// v10Entries builds on v9 by sharding index rows to reduce their size.
type v10Entries struct {
	rowShards uint32
}

func (s v10Entries) GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, _ string) ([]Entry, error) {
	seriesID := labelsSeriesID(labels)

	// read first 32 bits of the hash and use this to calculate the shard
	shard := binary.BigEndian.Uint32(seriesID) % s.rowShards

	entries := []Entry{
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
		entries = append(entries, Entry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v10Entries) GetChunkWriteEntries(bucket Bucket, _ string, labels labels.Labels, chunkID string) ([]Entry, error) {
	seriesID := labelsSeriesID(labels)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []Entry{
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

func (s v10Entries) GetReadMetricQueries(bucket Bucket, metricName string) ([]Query, error) {
	result := make([]Query, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, Query{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%02d:%s:%s", i, bucket.hashKey, metricName),
		})
	}
	return result, nil
}

func (s v10Entries) GetReadMetricLabelQueries(bucket Bucket, metricName string, labelName string) ([]Query, error) {
	result := make([]Query, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, Query{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%02d:%s:%s:%s", i, bucket.hashKey, metricName, labelName),
		})
	}
	return result, nil
}

func (s v10Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName string, labelName string, labelValue string) ([]Query, error) {
	valueHash := sha256bytes(labelValue)
	result := make([]Query, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, Query{
			TableName:        bucket.tableName,
			HashValue:        fmt.Sprintf("%02d:%s:%s:%s", i, bucket.hashKey, metricName, labelName),
			RangeValuePrefix: rangeValuePrefix(valueHash),
			ValueEqual:       []byte(labelValue),
		})
	}
	return result, nil
}

func (v10Entries) GetChunksForSeries(bucket Bucket, seriesID []byte) ([]Query, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []Query{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + string(seriesID),
			RangeValueStart: rangeValuePrefix(encodedFromBytes),
		},
	}, nil
}

func (v10Entries) GetLabelNamesForSeries(_ Bucket, _ []byte) ([]Query, error) {
	return nil, ErrNotSupported
}

// FilterReadQueries will return only queries that match a certain shard
func (v10Entries) FilterReadQueries(queries []Query, shard *astmapper.ShardAnnotation) (matches []Query) {
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

func (s v11Entries) GetLabelWriteEntries(bucket Bucket, metricName string, labels labels.Labels, _ string) ([]Entry, error) {
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
	entries := []Entry{
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
		entries = append(entries, Entry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey, metricName, v.Name),
			RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
			Value:      []byte(v.Value),
		})
	}

	return entries, nil
}

func (v11Entries) GetLabelNamesForSeries(bucket Bucket, seriesID []byte) ([]Query, error) {
	return []Query{
		{
			TableName: bucket.tableName,
			HashValue: string(seriesID),
		},
	}, nil
}

type v12Entries struct {
	v11Entries
}

type v13Entries struct {
	v12Entries
}
