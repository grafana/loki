package chunk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
)

var (
	chunkTimeRangeKeyV1  = []byte{'1'}
	chunkTimeRangeKeyV2  = []byte{'2'}
	chunkTimeRangeKeyV3  = []byte{'3'}
	chunkTimeRangeKeyV4  = []byte{'4'}
	chunkTimeRangeKeyV5  = []byte{'5'}
	metricNameRangeKeyV1 = []byte{'6'}

	// For v9 schema
	seriesRangeKeyV1      = []byte{'7'}
	labelSeriesRangeKeyV1 = []byte{'8'}

	// ErrNotSupported when a schema doesn't support that particular lookup.
	ErrNotSupported = errors.New("not supported")
)

// Schema interface defines methods to calculate the hash and range keys needed
// to write or read chunks from the external index.
type Schema interface {
	// When doing a write, use this method to return the list of entries you should write to.
	GetWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error)

	// Should only be used with the seriesStore. TODO: Make seriesStore implement a different interface altogether.
	GetLabelWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error)
	GetChunkWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error)
	GetLabelEntryCacheKeys(from, through model.Time, userID string, labels model.Metric) []string

	// When doing a read, use these methods to return the list of entries you should query
	GetReadQueriesForMetric(from, through model.Time, userID string, metricName model.LabelValue) ([]IndexQuery, error)
	GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error)
	GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error)

	// If the query resulted in series IDs, use this method to find chunks.
	GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error)
}

// IndexQuery describes a query for entries
type IndexQuery struct {
	TableName string
	HashValue string

	// One of RangeValuePrefix or RangeValueStart might be set:
	// - If RangeValuePrefix is not nil, must read all keys with that prefix.
	// - If RangeValueStart is not nil, must read all keys from there onwards.
	// - If neither is set, must read all keys for that row.
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

// schema implements Schema given a bucketing function and and set of range key callbacks
type schema struct {
	buckets func(from, through model.Time, userID string) []Bucket
	entries entries
}

func (s schema) GetWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
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

func (s schema) GetLabelWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	var result []IndexEntry

	for _, bucket := range s.buckets(from, through, userID) {
		entries, err := s.entries.GetLabelWriteEntries(bucket, metricName, labels, chunkID)
		if err != nil {
			return nil, err
		}
		result = append(result, entries...)
	}
	return result, nil
}

func (s schema) GetChunkWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
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

// Should only used for v9Schema
func (s schema) GetLabelEntryCacheKeys(from, through model.Time, userID string, labels model.Metric) []string {
	var result []string
	for _, bucket := range s.buckets(from, through, userID) {
		key := strings.Join([]string{
			bucket.tableName,
			bucket.hashKey,
			string(sha256bytes(labels.String())),
		},
			"-",
		)

		result = append(result, key)
	}

	return result
}

func (s schema) GetReadQueriesForMetric(from, through model.Time, userID string, metricName model.LabelValue) ([]IndexQuery, error) {
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

func (s schema) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
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

func (s schema) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
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

func (s schema) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
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

type entries interface {
	GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error)
	GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error)
	GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error)

	GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error)
	GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error)
	GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error)
	GetChunksForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error)
}

// original entries:
// - hash key: <userid>:<bucket>:<metric name>
// - range key: <label name>\0<label value>\0<chunk name>

type originalEntries struct{}

func (originalEntries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	result := []IndexEntry{}
	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}
		if strings.ContainsRune(string(value), '\x00') {
			return nil, fmt.Errorf("label values cannot contain null byte")
		}
		result = append(result, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(metricName),
			RangeValue: encodeRangeKey([]byte(key), []byte(value), chunkIDBytes),
		})
	}
	return result, nil
}

func (originalEntries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}
func (originalEntries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (originalEntries) GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + string(metricName),
			RangeValuePrefix: nil,
		},
	}, nil
}

func (originalEntries) GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + string(metricName),
			RangeValuePrefix: encodeRangeKey([]byte(labelName)),
		},
	}, nil
}

func (originalEntries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	if strings.ContainsRune(string(labelValue), '\x00') {
		return nil, fmt.Errorf("label values cannot contain null byte")
	}
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + string(metricName),
			RangeValuePrefix: encodeRangeKey([]byte(labelName), []byte(labelValue)),
		},
	}, nil
}

func (originalEntries) GetChunksForSeries(_ Bucket, _ []byte) ([]IndexQuery, error) {
	return nil, ErrNotSupported
}

// v3Schema went to base64 encoded label values & a version ID
// - range key: <label name>\0<base64(label value)>\0<chunk name>\0<version 1>

type base64Entries struct {
	originalEntries
}

func (base64Entries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	result := []IndexEntry{}
	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}

		encodedBytes := encodeBase64Value(value)
		result = append(result, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(metricName),
			RangeValue: encodeRangeKey([]byte(key), encodedBytes, chunkIDBytes, chunkTimeRangeKeyV1),
		})
	}
	return result, nil
}

func (base64Entries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}
func (base64Entries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (base64Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	encodedBytes := encodeBase64Value(labelValue)
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        bucket.hashKey + ":" + string(metricName),
			RangeValuePrefix: encodeRangeKey([]byte(labelName), encodedBytes),
		},
	}, nil
}

// v4 schema went to two schemas in one:
// 1) - hash key: <userid>:<hour bucket>:<metric name>:<label name>
//    - range key: \0<base64(label value)>\0<chunk name>\0<version 2>
// 2) - hash key: <userid>:<hour bucket>:<metric name>
//    - range key: \0\0<chunk name>\0<version 3>
type labelNameInHashKeyEntries struct{}

func (labelNameInHashKeyEntries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	entries := []IndexEntry{
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(metricName),
			RangeValue: encodeRangeKey(nil, nil, chunkIDBytes, chunkTimeRangeKeyV2),
		},
	}

	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}
		encodedBytes := encodeBase64Value(value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, key),
			RangeValue: encodeRangeKey(nil, encodedBytes, chunkIDBytes, chunkTimeRangeKeyV1),
		})
	}

	return entries, nil
}

func (labelNameInHashKeyEntries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}
func (labelNameInHashKeyEntries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (labelNameInHashKeyEntries) GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + string(metricName),
		},
	}, nil
}

func (labelNameInHashKeyEntries) GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (labelNameInHashKeyEntries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	encodedBytes := encodeBase64Value(labelValue)
	return []IndexQuery{
		{
			TableName:        bucket.tableName,
			HashValue:        fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValuePrefix: encodeRangeKey(nil, encodedBytes),
		},
	}, nil
}

func (labelNameInHashKeyEntries) GetChunksForSeries(_ Bucket, _ []byte) ([]IndexQuery, error) {
	return nil, ErrNotSupported
}

// v5 schema is an extension of v4, with the chunk end time in the
// range key to improve query latency.  However, it did it wrong
// so the chunk end times are ignored.
type v5Entries struct{}

func (v5Entries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(metricName),
			RangeValue: encodeRangeKey(encodedThroughBytes, nil, chunkIDBytes, chunkTimeRangeKeyV3),
		},
	}

	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}
		encodedValueBytes := encodeBase64Value(value)
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, key),
			RangeValue: encodeRangeKey(encodedThroughBytes, encodedValueBytes, chunkIDBytes, chunkTimeRangeKeyV4),
		})
	}

	return entries, nil
}

func (v5Entries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}
func (v5Entries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (v5Entries) GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + string(metricName),
		},
	}, nil
}

func (v5Entries) GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v5Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, _ model.LabelValue) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v5Entries) GetChunksForSeries(_ Bucket, _ []byte) ([]IndexQuery, error) {
	return nil, ErrNotSupported
}

// v6Entries fixes issues with v5 time encoding being wrong (see #337), and
// moves label value out of range key (see #199).
type v6Entries struct{}

func (v6Entries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	chunkIDBytes := []byte(chunkID)
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(metricName),
			RangeValue: encodeRangeKey(encodedThroughBytes, nil, chunkIDBytes, chunkTimeRangeKeyV3),
		},
	}

	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, key),
			RangeValue: encodeRangeKey(encodedThroughBytes, nil, chunkIDBytes, chunkTimeRangeKeyV5),
			Value:      []byte(value),
		})
	}

	return entries, nil
}

func (v6Entries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}
func (v6Entries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (v6Entries) GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + string(metricName),
			RangeValueStart: encodeRangeKey(encodedFromBytes),
		},
	}, nil
}

func (v6Entries) GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValueStart: encodeRangeKey(encodedFromBytes),
		},
	}, nil
}

func (v6Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValueStart: encodeRangeKey(encodedFromBytes),
			ValueEqual:      []byte(labelValue),
		},
	}, nil
}

func (v6Entries) GetChunksForSeries(_ Bucket, _ []byte) ([]IndexQuery, error) {
	return nil, ErrNotSupported
}

// v9Entries adds a layer of indirection between labels -> series -> chunks.
type v9Entries struct {
}

func (v9Entries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (v9Entries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	seriesID := sha256bytes(labels.String())

	entries := []IndexEntry{
		// Entry for metricName -> seriesID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(metricName),
			RangeValue: encodeRangeKey(seriesID, nil, nil, seriesRangeKeyV1),
		},
	}

	// Entries for metricName:labelName -> hash(value):seriesID
	// We use a hash of the value to limit its length.
	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}
		valueHash := sha256bytes(string(value))
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, key),
			RangeValue: encodeRangeKey(valueHash, seriesID, nil, labelSeriesRangeKeyV1),
			Value:      []byte(value),
		})
	}

	return entries, nil
}

func (v9Entries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	seriesID := sha256bytes(labels.String())
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		// Entry for seriesID -> chunkID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(seriesID),
			RangeValue: encodeRangeKey(encodedThroughBytes, nil, []byte(chunkID), chunkTimeRangeKeyV3),
		},
	}

	return entries, nil
}

func (v9Entries) GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: bucket.hashKey + ":" + string(metricName),
		},
	}, nil
}

func (v9Entries) GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	return []IndexQuery{
		{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
		},
	}, nil
}

func (v9Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	valueHash := sha256bytes(string(labelValue))
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, labelName),
			RangeValueStart: encodeRangeKey(valueHash),
			ValueEqual:      []byte(labelValue),
		},
	}, nil
}

func (v9Entries) GetChunksForSeries(bucket Bucket, seriesID []byte) ([]IndexQuery, error) {
	encodedFromBytes := encodeTime(bucket.from)
	return []IndexQuery{
		{
			TableName:       bucket.tableName,
			HashValue:       bucket.hashKey + ":" + string(seriesID),
			RangeValueStart: encodeRangeKey(encodedFromBytes),
		},
	}, nil
}

// v10Entries builds on v9 by sharding index rows to reduce their size.
type v10Entries struct {
	rowShards uint32
}

func (v10Entries) GetWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, ErrNotSupported
}

func (s v10Entries) GetLabelWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	seriesID := sha256bytes(labels.String())

	// read first 32 bits of the hash and use this to calculate the shard
	shard := binary.BigEndian.Uint32(seriesID) % s.rowShards

	entries := []IndexEntry{
		// Entry for metricName -> seriesID
		{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s", shard, bucket.hashKey, string(metricName)),
			RangeValue: encodeRangeKey(seriesID, nil, nil, seriesRangeKeyV1),
		},
	}

	// Entries for metricName:labelName -> hash(value):seriesID
	// We use a hash of the value to limit its length.
	for key, value := range labels {
		if key == model.MetricNameLabel {
			continue
		}
		valueHash := sha256bytes(string(value))
		entries = append(entries, IndexEntry{
			TableName:  bucket.tableName,
			HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey, metricName, key),
			RangeValue: encodeRangeKey(valueHash, seriesID, nil, labelSeriesRangeKeyV1),
			Value:      []byte(value),
		})
	}

	return entries, nil
}

func (v10Entries) GetChunkWriteEntries(bucket Bucket, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	seriesID := sha256bytes(labels.String())
	encodedThroughBytes := encodeTime(bucket.through)

	entries := []IndexEntry{
		// Entry for seriesID -> chunkID
		{
			TableName:  bucket.tableName,
			HashValue:  bucket.hashKey + ":" + string(seriesID),
			RangeValue: encodeRangeKey(encodedThroughBytes, nil, []byte(chunkID), chunkTimeRangeKeyV3),
		},
	}

	return entries, nil
}

func (s v10Entries) GetReadMetricQueries(bucket Bucket, metricName model.LabelValue) ([]IndexQuery, error) {
	result := make([]IndexQuery, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, IndexQuery{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%02d:%s:%s", i, bucket.hashKey, string(metricName)),
		})
	}
	return result, nil
}

func (s v10Entries) GetReadMetricLabelQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	result := make([]IndexQuery, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, IndexQuery{
			TableName: bucket.tableName,
			HashValue: fmt.Sprintf("%02d:%s:%s:%s", i, bucket.hashKey, metricName, labelName),
		})
	}
	return result, nil
}

func (s v10Entries) GetReadMetricLabelValueQueries(bucket Bucket, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	valueHash := sha256bytes(string(labelValue))
	result := make([]IndexQuery, 0, s.rowShards)
	for i := uint32(0); i < s.rowShards; i++ {
		result = append(result, IndexQuery{
			TableName:       bucket.tableName,
			HashValue:       fmt.Sprintf("%02d:%s:%s:%s", i, bucket.hashKey, metricName, labelName),
			RangeValueStart: encodeRangeKey(valueHash),
			ValueEqual:      []byte(labelValue),
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
			RangeValueStart: encodeRangeKey(encodedFromBytes),
		},
	}, nil
}
