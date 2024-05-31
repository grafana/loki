package logproto

import (
	"testing"

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
