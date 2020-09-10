package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareStreams(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected json.RawMessage
		actual   json.RawMessage
		err      error
	}{
		{
			name:     "no streams",
			expected: json.RawMessage(`[]`),
			actual:   json.RawMessage(`[]`),
		},
		{
			name: "no streams in actual response",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"]]}
						]`),
			actual: json.RawMessage(`[]`),
			err:    errors.New("expected 1 streams but got 0"),
		},
		{
			name: "extra stream in actual response",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"]]}
						]`),
			actual: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"]]},
							{"stream":{"foo1":"bar1"},"values":[["1","1"]]}
						]`),
			err: errors.New("expected 1 streams but got 2"),
		},
		{
			name: "same number of streams but with different labels",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"]]}
						]`),
			actual: json.RawMessage(`[
							{"stream":{"foo1":"bar1"},"values":[["1","1"]]}
						]`),
			err: errors.New("expected stream {foo=\"bar\"} missing from actual response"),
		},
		{
			name: "difference in number of samples",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["2","2"]]}
						]`),
			actual: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"]]}
						]`),
			err: errors.New("expected 2 values for stream {foo=\"bar\"} but got 1"),
		},
		{
			name: "difference in sample timestamp",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["2","2"]]}
						]`),
			actual: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["3","2"]]}
						]`),
			err: errors.New("expected timestamp 2 but got 3 for stream {foo=\"bar\"}"),
		},
		{
			name: "difference in sample value",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["2","2"]]}
						]`),
			actual: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["2","3"]]}
						]`),
			err: errors.New("expected line 2 for timestamp 2 but got 3 for stream {foo=\"bar\"}"),
		},
		{
			name: "correct samples",
			expected: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["2","2"]]}
						]`),
			actual: json.RawMessage(`[
							{"stream":{"foo":"bar"},"values":[["1","1"],["2","2"]]}
						]`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := compareStreams(tc.expected, tc.actual, 0)
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.err.Error(), err.Error())
		})
	}
}
