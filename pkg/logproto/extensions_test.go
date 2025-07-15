package logproto

import (
	"testing"

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

func TestQueryPatternsRequest_GetSampleQuery(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		step          int64
		expected      string
		expectedError bool
	}{
		{
			name:  "simple selector with service_name",
			query: `{service_name="test-service"}`,
			step:  60000, // 1 minute in milliseconds
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__="test-service"} | logfmt | ` +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [1m0s]))`,
		},
		{
			name:  "selector with service_name and additional labels",
			query: `{service_name="test-service", env="prod", cluster="us-east-1"}`,
			step:  300000, // 5 minutes in milliseconds
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__="test-service"} | logfmt | ` +
				"env=\"prod\" | cluster=\"us-east-1\" | " +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [5m0s]))`,
		},
		{
			name:  "selector with service_name and additional labels and match types",
			query: `{service_name="test-service", env=~"prod", cluster!="us-east-1", foo!~"bar"}`,
			step:  300000, // 5 minutes in milliseconds
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__="test-service"} | logfmt | ` +
				"env=~\"prod\" | cluster!=\"us-east-1\" | foo!~\"bar\" | " +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [5m0s]))`,
		},
		{
			name:  "small step gets minimum 10s window",
			query: `{service_name="test-service"}`,
			step:  5000, // 5 seconds in milliseconds
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__="test-service"} | logfmt | ` +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [10s]))`,
		},
		{
			name:  "simple regex selector with service_name",
			query: `{service_name=~"test-service"}`,
			step:  10000, // 10 seconds in milliseconds
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__=~"test-service"} | logfmt | ` +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [10s]))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &QueryPatternsRequest{
				Query: tc.query,
				Step:  tc.step,
			}

			result, err := req.GetSampleQuery()

			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestQueryPatternsRequest_GetSampleQuery_NoServiceName(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expected      string
		expectedError bool
	}{
		{
			name:  "no service_name label",
			query: `{env="prod", cluster="us-east-1"}`,
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__=~".+"} | logfmt | ` +
				"env=\"prod\" | cluster=\"us-east-1\" | " +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [1m0s]))`,
		},
		{
			name:  "no service_name label, mixed match types",
			query: `{env!="prod", cluster=~"us-east-1", app!~"foo"}`,
			expected: `sum by (decoded_pattern, detected_level) (sum_over_time({__pattern__=~".+"} | logfmt | ` +
				"env!=\"prod\" | cluster=~\"us-east-1\" | app!~\"foo\" | " +
				"label_format decoded_pattern=`{{urldecode .detected_pattern}}` " +
				`| unwrap count [1m0s]))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &QueryPatternsRequest{
				Query: tc.query,
				Step:  60000,
			}

			result, err := req.GetSampleQuery()
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestQueryPatternsRequest_GetSampleQuery_InvalidQuery(t *testing.T) {
	req := &QueryPatternsRequest{
		Query: `{invalid query syntax`,
		Step:  60000,
	}

	_, err := req.GetSampleQuery()
	require.Error(t, err)
}
