package index

import (
	"bytes"
	"fmt"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func TestDailyBuckets(t *testing.T) {
	const (
		userID     = "0"
		metricName = model.LabelValue("name")
		tableName  = "table"
	)
	cfg := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: tableName}},
	}

	type args struct {
		from    model.Time
		through model.Time
	}
	tests := []struct {
		name string
		args args
		want []Bucket
	}{
		{
			"0 day window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(0),
			},
			[]Bucket{{
				from:       0,
				through:    0,
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
		{
			"6 hour window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(6 * 3600),
			},
			[]Bucket{{
				from:       0,
				through:    (6 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
		{
			"1 day window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(24 * 3600),
			},
			[]Bucket{{
				from:       0,
				through:    (24 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}, {
				from:       0,
				through:    0,
				tableName:  "table",
				hashKey:    "0:d1",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
		{
			"window spanning 3 days with non-zero start",
			args{
				from:    model.TimeFromUnix(6 * 3600),
				through: model.TimeFromUnix((2 * 24 * 3600) + (12 * 3600)),
			},
			[]Bucket{{
				from:       (6 * 3600) * 1000,  // ms
				through:    (24 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}, {
				from:       0,
				through:    (24 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d1",
				bucketSize: uint32(millisecondsInDay),
			}, {
				from:       0,
				through:    (12 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d2",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dailyBuckets(cfg)(tt.args.from, tt.args.through, userID)
			assert.Equal(t, tt.want, got)
		})
	}
}

type ByHashRangeKey []Entry

func (a ByHashRangeKey) Len() int      { return len(a) }
func (a ByHashRangeKey) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByHashRangeKey) Less(i, j int) bool {
	if a[i].HashValue != a[j].HashValue {
		return a[i].HashValue < a[j].HashValue
	}
	return bytes.Compare(a[i].RangeValue, a[j].RangeValue) < 0
}

const table = "table"

func mustMakeSchema(schemaName string) SeriesStoreSchema {
	s, err := CreateSchema(config.PeriodConfig{
		Schema: schemaName,
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: table}},
	})
	if err != nil {
		panic(err)
	}
	return s
}

// range value types
const (
	_ = iota
	MetricNameRangeValue
	ChunkTimeRangeValue
	SeriesRangeValue
)

func BenchmarkEncodeLabelsJson(b *testing.B) {
	decoded := &labels.Labels{}
	lbs := labels.FromMap(map[string]string{
		"foo":      "bar",
		"fuzz":     "buzz",
		"cluster":  "test",
		"test":     "test1",
		"instance": "cortex-01",
		"bar":      "foo",
		"version":  "0.1",
	})
	json := jsoniter.ConfigFastest
	var data []byte
	var err error
	for n := 0; n < b.N; n++ {
		data, err = json.Marshal(lbs)
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(data, decoded)
		if err != nil {
			panic(err)
		}
	}
	b.Log("data size", len(data))
	b.Log("decode", decoded)
}

func BenchmarkEncodeLabelsString(b *testing.B) {
	var decoded labels.Labels
	lbs := labels.FromMap(map[string]string{
		"foo":      "bar",
		"fuzz":     "buzz",
		"cluster":  "test",
		"test":     "test1",
		"instance": "cortex-01",
		"bar":      "foo",
		"version":  "0.1",
	})
	var data []byte
	var err error
	for n := 0; n < b.N; n++ {
		data = []byte(lbs.String())
		decoded, err = parser.ParseMetric(string(data))
		if err != nil {
			panic(err)
		}
	}
	b.Log("data size", len(data))
	b.Log("decode", decoded)
}

func TestV10IndexQueries(t *testing.T) {
	fromShards := func(n int) (res []Query) {
		for i := 0; i < n; i++ {
			res = append(res, Query{
				TableName:       "tbl",
				HashValue:       fmt.Sprintf("%02d:%s:%s:%s", i, "hash", "metric", "label"),
				RangeValueStart: []byte(fmt.Sprint(i)),
				ValueEqual:      []byte(fmt.Sprint(i)),
			})
		}
		return res
	}

	testExprs := []struct {
		name     string
		queries  []Query
		shard    *astmapper.ShardAnnotation
		expected []Query
	}{
		{
			name:     "passthrough when no shard specified",
			queries:  fromShards(2),
			shard:    nil,
			expected: fromShards(2),
		},
		{
			name:    "out of bounds shard returns 0 matches",
			queries: fromShards(2),
			shard: &astmapper.ShardAnnotation{
				Shard: 3,
			},
			expected: nil,
		},
		{
			name:    "return correct shard",
			queries: fromShards(3),
			shard: &astmapper.ShardAnnotation{
				Shard: 1,
			},
			expected: []Query{fromShards(2)[1]},
		},
	}

	for _, c := range testExprs {
		t.Run(c.name, func(t *testing.T) {
			s := v10Entries{}
			filtered := s.FilterReadQueries(c.queries, c.shard)
			require.Equal(t, c.expected, filtered)
		})
	}
}
