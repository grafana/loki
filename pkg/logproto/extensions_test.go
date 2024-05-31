package logproto

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestShard_SpaceFor(t *testing.T) {
	target := uint64(100)
	shard := Shard{
		Stats: &IndexStatsResponse{
			Bytes: 50,
		},
	}

	for _, tc := range []struct {
		desc  string
		bytes uint64
		exp   bool
	}{
		{
			desc:  "full shard",
			bytes: 50,
			exp:   true,
		},
		{
			desc:  "overflow equal to underflow accepts",
			bytes: 100,
			exp:   true,
		},
		{
			desc:  "overflow",
			bytes: 101,
			exp:   false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, shard.SpaceFor(&IndexStatsResponse{Bytes: tc.bytes}, target), tc.exp)
		})
	}
}

func TestQueryPatternsResponse_UnmarshalJSON(t *testing.T) {
	mockData := []byte(`{
		"status": "success",
		"data": [
			{
				"pattern": "foo <*> bar",
				"samples": [[1609459200, 10], [1609545600, 15]]
			},
			{
				"pattern": "foo <*> buzz",
				"samples": [[1609459200, 20], [1609545600, 25]]
			}
		]
	}`)

	expectedSeries := []*PatternSeries{
		NewPatternSeries("foo <*> bar", []*PatternSample{
			{Timestamp: model.TimeFromUnix(1609459200), Value: 10},
			{Timestamp: model.TimeFromUnix(1609545600), Value: 15},
		}),
		NewPatternSeries("foo <*> buzz", []*PatternSample{
			{Timestamp: model.TimeFromUnix(1609459200), Value: 20},
			{Timestamp: model.TimeFromUnix(1609545600), Value: 25},
		}),
	}

	r := &QueryPatternsResponse{}
	err := r.UnmarshalJSON(mockData)

	require.Nil(t, err)
	require.Equal(t, expectedSeries, r.Series)
}

func TestQuerySamplesResponse_UnmarshalJSON(t *testing.T) {
	mockData := []byte(`{
    "status": "success",
    "data": [{
      "metric": {
        "foo": "bar"
      },
      "values": [
        [0.001, "1"],
        [0.002, "2"]
      ]
    },
    {
      "metric": {
        "foo": "baz",
        "bar": "qux"
      },
      "values": [
        [0.003, "3"],
        [0.004, "4"]
      ]
    }]
  }`)

	lbls1, err := syntax.ParseLabels(`{foo="bar"}`)
	require.NoError(t, err)
	lbls2, err := syntax.ParseLabels(`{bar="qux", foo="baz"}`)
	require.NoError(t, err)

	expectedSamples := []Series{
		{
			Labels: lbls1.String(),
			Samples: []Sample{
				{Timestamp: 1e6, Value: 1}, // 1ms after epoch in ns
				{Timestamp: 2e6, Value: 2}, // 2ms after epoch in ns
			},
			StreamHash: lbls1.Hash(),
		},
		{
			Labels: lbls2.String(),
			Samples: []Sample{
				{Timestamp: 3e6, Value: 3}, // 3ms after epoch in ns
				{Timestamp: 4e6, Value: 4}, // 4ms after epoch in ns
			},
			StreamHash: lbls2.Hash(),
		},
	}

	r := &QuerySamplesResponse{}
	err = r.UnmarshalJSON(mockData)

	require.Nil(t, err)
	require.Equal(t, expectedSamples, r.Series)
}
