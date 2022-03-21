package chunk

import (
	"bytes"
	"fmt"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/querier/astmapper"
)

type ByHashRangeKey []IndexEntry

func (a ByHashRangeKey) Len() int      { return len(a) }
func (a ByHashRangeKey) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByHashRangeKey) Less(i, j int) bool {
	if a[i].HashValue != a[j].HashValue {
		return a[i].HashValue < a[j].HashValue
	}
	return bytes.Compare(a[i].RangeValue, a[j].RangeValue) < 0
}

const table = "table"

func mustMakeSchema(schemaName string) BaseSchema {
	s, err := PeriodConfig{
		Schema:      schemaName,
		IndexTables: PeriodicTableConfig{Prefix: table},
	}.CreateSchema()
	if err != nil {
		panic(err)
	}
	return s
}

func makeSeriesStoreSchema(schemaName string) SeriesStoreSchema {
	return mustMakeSchema(schemaName).(SeriesStoreSchema)
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
	fromShards := func(n int) (res []IndexQuery) {
		for i := 0; i < n; i++ {
			res = append(res, IndexQuery{
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
		queries  []IndexQuery
		shard    *astmapper.ShardAnnotation
		expected []IndexQuery
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
			expected: []IndexQuery{fromShards(2)[1]},
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
