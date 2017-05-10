package chunk

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/test"
	"github.com/weaveworks/cortex/pkg/util"
)

type mockSchema int

func (mockSchema) GetWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return nil, nil
}
func (mockSchema) GetReadQueries(from, through model.Time, userID string) ([]IndexQuery, error) {
	return nil, nil
}
func (mockSchema) GetReadQueriesForMetric(from, through model.Time, userID string, metricName model.LabelValue) ([]IndexQuery, error) {
	return nil, nil
}
func (mockSchema) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	return nil, nil
}
func (mockSchema) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	return nil, nil
}

type ByHashRangeKey []IndexEntry

func (a ByHashRangeKey) Len() int      { return len(a) }
func (a ByHashRangeKey) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByHashRangeKey) Less(i, j int) bool {
	if a[i].HashValue != a[j].HashValue {
		return a[i].HashValue < a[j].HashValue
	}
	return bytes.Compare(a[i].RangeValue, a[j].RangeValue) < 0
}

func mergeResults(rss ...[]IndexEntry) []IndexEntry {
	results := []IndexEntry{}
	for _, rs := range rss {
		results = append(results, rs...)
	}
	return results
}

func TestSchemaHashKeys(t *testing.T) {
	mkResult := func(tableName, fmtStr string, from, through int) []IndexEntry {
		want := []IndexEntry{}
		for i := from; i < through; i++ {
			want = append(want, IndexEntry{
				TableName: tableName,
				HashValue: fmt.Sprintf(fmtStr, i),
			})
		}
		return want
	}

	const (
		userID         = "userid"
		table          = "table"
		periodicPrefix = "periodicPrefix"
	)

	cfg := SchemaConfig{
		PeriodicTableConfig: PeriodicTableConfig{
			OriginalTableName:    table,
			UsePeriodicTables:    true,
			TablePrefix:          periodicPrefix,
			TablePeriod:          2 * 24 * time.Hour,
			PeriodicTableStartAt: util.NewDayValue(model.TimeFromUnix(5 * 24 * 60 * 60)),
		},
	}
	compositeSchema := func(dailyBucketsFrom model.Time) Schema {
		cfgCp := cfg
		cfgCp.DailyBucketsFrom = util.NewDayValue(dailyBucketsFrom)
		schema, err := newCompositeSchema(cfgCp)
		if err != nil {
			t.Fatal(err)
		}
		return schema
	}
	hourlyBuckets := v1Schema(cfg)
	dailyBuckets := v3Schema(cfg)
	labelBuckets := v4Schema(cfg)
	metric := model.Metric{
		model.MetricNameLabel: "foo",
		"bar": "baz",
	}
	chunkID := "chunkID"

	for i, tc := range []struct {
		Schema
		from, through int64
		metricName    string
		want          []IndexEntry
	}{
		// Basic test case for the various bucketing schemes
		{
			hourlyBuckets,
			0, (30 * 60) - 1, "foo", // chunk is smaller than bucket
			mkResult(table, "userid:%d:foo", 0, 1),
		},
		{
			hourlyBuckets,
			0, (3 * 24 * 60 * 60) - 1, "foo",
			mkResult(table, "userid:%d:foo", 0, 3*24),
		},
		{
			hourlyBuckets,
			0, 30 * 60, "foo", // chunk is smaller than bucket
			mkResult(table, "userid:%d:foo", 0, 1),
		},
		{
			dailyBuckets,
			0, (3 * 24 * 60 * 60) - 1, "foo",
			mkResult(table, "userid:d%d:foo", 0, 3),
		},
		{
			labelBuckets,
			0, (3 * 24 * 60 * 60) - 1, "foo",
			mergeResults(
				mkResult(table, "userid:d%d:foo", 0, 3),
				mkResult(table, "userid:d%d:foo:bar", 0, 3),
			),
		},

		// Buckets are by hour until we reach the `dailyBucketsFrom`, after which they are by day.
		{
			compositeSchema(model.TimeFromUnix(0).Add(1 * 24 * time.Hour)),
			0, (3 * 24 * 60 * 60) - 1, "foo",
			mergeResults(
				mkResult(table, "userid:%d:foo", 0, 1*24),
				mkResult(table, "userid:d%d:foo", 1, 3),
			),
		},

		// Only the day part of `dailyBucketsFrom` matters, not the time part.
		{
			compositeSchema(model.TimeFromUnix(0).Add(2*24*time.Hour) - 1),
			0, (3 * 24 * 60 * 60) - 1, "foo",
			mergeResults(
				mkResult(table, "userid:%d:foo", 0, 1*24),
				mkResult(table, "userid:d%d:foo", 1, 3),
			),
		},

		// Moving dailyBucketsFrom to the previous day compared to the above makes 24 1-hour buckets disappear.
		{
			compositeSchema(model.TimeFromUnix(0).Add(1*24*time.Hour) - 1),
			0, (3 * 24 * 60 * 60) - 1, "foo",
			mkResult(table, "userid:d%d:foo", 0, 3),
		},

		// If `dailyBucketsFrom` is after the interval, everything will be bucketed by hour.
		{
			compositeSchema(model.TimeFromUnix(0).Add(99 * 24 * time.Hour)),
			0, (2 * 24 * 60 * 60) - 1, "foo",
			mkResult(table, "userid:%d:foo", 0, 2*24),
		},

		// Should only return daily buckets when dailyBucketsFrom is before the interval.
		{
			compositeSchema(model.TimeFromUnix(0)),
			1 * 24 * 60 * 60, (3 * 24 * 60 * 60) - 1, "foo",
			mkResult(table, "userid:d%d:foo", 1, 3),
		},

		// Basic weekly- ables.
		{
			compositeSchema(model.TimeFromUnix(0)),
			5 * 24 * 60 * 60, (10 * 24 * 60 * 60) - 1, "foo",
			mergeResults(
				mkResult(periodicPrefix+"2", "userid:d%d:foo", 5, 6),
				mkResult(periodicPrefix+"3", "userid:d%d:foo", 6, 8),
				mkResult(periodicPrefix+"4", "userid:d%d:foo", 8, 10),
			),
		},

		// Daily buckets + weekly tables.
		{
			compositeSchema(model.TimeFromUnix(0)),
			0, (10 * 24 * 60 * 60) - 1, "foo",
			mergeResults(
				mkResult(table, "userid:d%d:foo", 0, 5),
				mkResult(periodicPrefix+"2", "userid:d%d:foo", 5, 6),
				mkResult(periodicPrefix+"3", "userid:d%d:foo", 6, 8),
				mkResult(periodicPrefix+"4", "userid:d%d:foo", 8, 10),
			),
		},

		// Houly Buckets, then daily buckets, then weekly tables.
		{
			compositeSchema(model.TimeFromUnix(2 * 24 * 60 * 60)),
			0, (10 * 24 * 60 * 60) - 1, "foo",
			mergeResults(
				mkResult(table, "userid:%d:foo", 0, 2*24),
				mkResult(table, "userid:d%d:foo", 2, 5),
				mkResult(periodicPrefix+"2", "userid:d%d:foo", 5, 6),
				mkResult(periodicPrefix+"3", "userid:d%d:foo", 6, 8),
				mkResult(periodicPrefix+"4", "userid:d%d:foo", 8, 10),
			),
		},
	} {
		t.Run(fmt.Sprintf("TestSchemaHashKeys[%d]", i), func(t *testing.T) {
			have, err := tc.Schema.GetWriteEntries(
				model.TimeFromUnix(tc.from), model.TimeFromUnix(tc.through),
				userID, model.LabelValue(tc.metricName),
				metric, chunkID,
			)
			if err != nil {
				t.Fatal(err)
			}
			for i := range have {
				have[i].RangeValue = nil
			}
			sort.Sort(ByHashRangeKey(have))
			sort.Sort(ByHashRangeKey(tc.want))
			if !reflect.DeepEqual(tc.want, have) {
				t.Fatalf("wrong hash buckets - %s", test.Diff(tc.want, have))
			}
		})
	}
}

// range value types
const (
	MetricNameRangeValue = iota + 1
	ChunkTimeRangeValue
)

// parseRangeValueType returns the type of rangeValue
func parseRangeValueType(rangeValue []byte) (int, error) {
	components := decodeRangeKey(rangeValue)
	switch {
	case len(components) < 3:
		return 0, fmt.Errorf("invalid range value: %x", rangeValue)

	// v1 & v2 chunk time range values
	case len(components) == 3:
		return ChunkTimeRangeValue, nil

	// chunk time range values
	case bytes.Equal(components[3], chunkTimeRangeKeyV1):
		return ChunkTimeRangeValue, nil

	case bytes.Equal(components[3], chunkTimeRangeKeyV2):
		return ChunkTimeRangeValue, nil

	case bytes.Equal(components[3], chunkTimeRangeKeyV3):
		return ChunkTimeRangeValue, nil

	case bytes.Equal(components[3], chunkTimeRangeKeyV4):
		return ChunkTimeRangeValue, nil

	case bytes.Equal(components[3], chunkTimeRangeKeyV5):
		return ChunkTimeRangeValue, nil

	// metric name range values
	case bytes.Equal(components[3], metricNameRangeKeyV1):
		return MetricNameRangeValue, nil

	default:
		return 0, fmt.Errorf("unrecognised range value type. version: '%v'", string(components[3]))
	}
}

func TestSchemaRangeKey(t *testing.T) {
	const (
		userID     = "userid"
		table      = "table"
		metricName = "foo"
		chunkID    = "chunkID"
	)

	var (
		cfg = SchemaConfig{
			PeriodicTableConfig: PeriodicTableConfig{
				OriginalTableName: table,
			},
		}
		hourlyBuckets = v1Schema(cfg)
		dailyBuckets  = v2Schema(cfg)
		base64Keys    = v3Schema(cfg)
		labelBuckets  = v4Schema(cfg)
		tsRangeKeys   = v5Schema(cfg)
		v6RangeKeys   = v6Schema(cfg)
		v7RangeKeys   = v7Schema(cfg)
		metric        = model.Metric{
			model.MetricNameLabel: metricName,
			"bar": "bary",
			"baz": "bazy",
		}
		fooSha1Hash = sha1.Sum([]byte("foo"))
	)

	mkEntries := func(hashKey string, callback func(labelName model.LabelName, labelValue model.LabelValue) ([]byte, []byte)) []IndexEntry {
		result := []IndexEntry{}
		for labelName, labelValue := range metric {
			if labelName == model.MetricNameLabel {
				continue
			}
			rangeValue, value := callback(labelName, labelValue)
			result = append(result, IndexEntry{
				TableName:  table,
				HashValue:  hashKey,
				RangeValue: rangeValue,
				Value:      value,
			})
		}
		return result
	}

	for i, tc := range []struct {
		Schema
		want []IndexEntry
	}{
		// Basic test case for the various bucketing schemes
		{
			hourlyBuckets,
			mkEntries("userid:0:foo", func(labelName model.LabelName, labelValue model.LabelValue) ([]byte, []byte) {
				return []byte(fmt.Sprintf("%s\x00%s\x00%s\x00", labelName, labelValue, chunkID)), nil
			}),
		},
		{
			dailyBuckets,
			mkEntries("userid:d0:foo", func(labelName model.LabelName, labelValue model.LabelValue) ([]byte, []byte) {
				return []byte(fmt.Sprintf("%s\x00%s\x00%s\x00", labelName, labelValue, chunkID)), nil
			}),
		},
		{
			base64Keys,
			mkEntries("userid:d0:foo", func(labelName model.LabelName, labelValue model.LabelValue) ([]byte, []byte) {
				encodedValue := base64.RawStdEncoding.EncodeToString([]byte(labelValue))
				return []byte(fmt.Sprintf("%s\x00%s\x00%s\x001\x00", labelName, encodedValue, chunkID)), nil
			}),
		},
		{
			labelBuckets,
			[]IndexEntry{
				{
					TableName:  table,
					HashValue:  "userid:d0:foo",
					RangeValue: []byte("\x00\x00chunkID\x002\x00"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:bar",
					RangeValue: []byte("\x00YmFyeQ\x00chunkID\x001\x00"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:baz",
					RangeValue: []byte("\x00YmF6eQ\x00chunkID\x001\x00"),
				},
			},
		},
		{
			tsRangeKeys,
			[]IndexEntry{
				{
					TableName:  table,
					HashValue:  "userid:d0:foo",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x003\x00"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:bar",
					RangeValue: []byte("0036ee7f\x00YmFyeQ\x00chunkID\x004\x00"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:baz",
					RangeValue: []byte("0036ee7f\x00YmF6eQ\x00chunkID\x004\x00"),
				},
			},
		},
		{
			v6RangeKeys,
			[]IndexEntry{
				{
					TableName:  table,
					HashValue:  "userid:d0:foo",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x003\x00"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:bar",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x005\x00"),
					Value:      []byte("bary"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:baz",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x005\x00"),
					Value:      []byte("bazy"),
				},
			},
		},
		{
			v7RangeKeys,
			[]IndexEntry{
				{
					TableName:  table,
					HashValue:  "userid:d0",
					RangeValue: append(encodeBase64Bytes(fooSha1Hash[:]), []byte("\x00\x00\x006\x00")...),
					Value:      []byte("foo"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x003\x00"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:bar",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x005\x00"),
					Value:      []byte("bary"),
				},
				{
					TableName:  table,
					HashValue:  "userid:d0:foo:baz",
					RangeValue: []byte("0036ee7f\x00\x00chunkID\x005\x00"),
					Value:      []byte("bazy"),
				},
			},
		},
	} {
		t.Run(fmt.Sprintf("TestSchameRangeKey[%d]", i), func(t *testing.T) {
			have, err := tc.Schema.GetWriteEntries(
				model.TimeFromUnix(0), model.TimeFromUnix(60*60)-1,
				userID, model.LabelValue(metricName),
				metric, chunkID,
			)
			if err != nil {
				t.Fatal(err)
			}
			sort.Sort(ByHashRangeKey(have))
			sort.Sort(ByHashRangeKey(tc.want))
			if !reflect.DeepEqual(tc.want, have) {
				t.Fatalf("wrong hash buckets - %s", test.Diff(tc.want, have))
			}

			// Test we can parse the resulting range keys
			for _, entry := range have {
				rangeValueType, err := parseRangeValueType(entry.RangeValue)
				require.NoError(t, err)

				switch rangeValueType {
				case MetricNameRangeValue:
					_, err := parseMetricNameRangeValue(entry.RangeValue, entry.Value)
					require.NoError(t, err)
				case ChunkTimeRangeValue:
					_, _, _, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
					require.NoError(t, err)
				}
			}
		})
	}
}
