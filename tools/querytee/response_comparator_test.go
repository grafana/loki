package querytee

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestCompareMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected json.RawMessage
		actual   json.RawMessage
		err      error
	}{
		{
			name:     "no metrics",
			expected: json.RawMessage(`[]`),
			actual:   json.RawMessage(`[]`),
		},
		{
			name: "no metrics in actual response",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"]]}
						]`),
			actual: json.RawMessage(`[]`),
			err:    errors.New("expected 1 metrics but got 0"),
		},
		{
			name: "extra metric in actual response",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"]]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"]]},
							{"metric":{"foo1":"bar1"},"values":[[1,"1"]]}
						]`),
			err: errors.New("expected 1 metrics but got 2"),
		},
		{
			name: "same number of metrics but with different labels",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"]]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo1":"bar1"},"values":[[1,"1"]]}
						]`),
			err: errors.New("expected metric {foo=\"bar\"} missing from actual response"),
		},
		{
			name: "difference in number of samples",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[2,"2"]]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"]]}
						]`),
			err: errors.New("expected 2 samples for metric {foo=\"bar\"} but got 1"),
		},
		{
			name: "difference in sample timestamp",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[2,"2"]]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[3,"2"]]}
						]`),
			// timestamps are parsed from seconds to ms which are then added to errors as is so adding 3 0s to expected error.
			err: errors.New("sample pair not matching for metric {foo=\"bar\"}: expected timestamp 2 but got 3"),
		},
		{
			name: "difference in sample value",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[2,"2"]]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[2,"3"]]}
						]`),
			err: errors.New("sample pair not matching for metric {foo=\"bar\"}: expected value 2 for timestamp 2 but got 3"),
		},
		{
			name: "correct samples",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[2,"2"]]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"values":[[1,"1"],[2,"2"]]}
						]`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareMatrix(tc.expected, tc.actual, SampleComparisonOptions{})
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.err.Error(), err.Error())
		})
	}
}

func TestCompareVector(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected json.RawMessage
		actual   json.RawMessage
		err      error
	}{
		{
			name:     "no metrics",
			expected: json.RawMessage(`[]`),
			actual:   json.RawMessage(`[]`),
		},
		{
			name: "no metrics in actual response",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
			actual: json.RawMessage(`[]`),
			err:    errors.New("expected 1 metrics but got 0"),
		},
		{
			name: "extra metric in actual response",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]},
							{"metric":{"foo1":"bar1"},"value":[1,"1"]}
						]`),
			err: errors.New("expected 1 metrics but got 2"),
		},
		{
			name: "same number of metrics but with different labels",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo1":"bar1"},"value":[1,"1"]}
						]`),
			err: errors.New("expected metric(s) [{foo=\"bar\"}] missing from actual response"),
		},
		{
			name: "difference in sample timestamp",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[2,"1"]}
						]`),
			err: errors.New("sample pair not matching for metric {foo=\"bar\"}: expected timestamp 1 but got 2"),
		},
		{
			name: "difference in sample value",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"2"]}
						]`),
			err: errors.New("sample pair not matching for metric {foo=\"bar\"}: expected value 1 for timestamp 1 but got 2"),
		},
		{
			name: "correct samples",
			expected: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
			actual: json.RawMessage(`[
							{"metric":{"foo":"bar"},"value":[1,"1"]}
						]`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareVector(tc.expected, tc.actual, SampleComparisonOptions{})
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.err.Error(), err.Error())
		})
	}
}

func TestCompareScalar(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected json.RawMessage
		actual   json.RawMessage
		err      error
	}{
		{
			name:     "difference in timestamp",
			expected: json.RawMessage(`[1,"1"]`),
			actual:   json.RawMessage(`[2,"1"]`),
			err:      errors.New("expected timestamp 1 but got 2"),
		},
		{
			name:     "difference in value",
			expected: json.RawMessage(`[1,"1"]`),
			actual:   json.RawMessage(`[1,"2"]`),
			err:      errors.New("expected value 1 for timestamp 1 but got 2"),
		},
		{
			name:     "correct values",
			expected: json.RawMessage(`[1,"1"]`),
			actual:   json.RawMessage(`[1,"1"]`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareScalar(tc.expected, tc.actual, SampleComparisonOptions{})
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.err.Error(), err.Error())
		})
	}
}

func TestCompareSamplesResponse(t *testing.T) {
	now := model.Now().String()
	for _, tc := range []struct {
		name              string
		tolerance         float64
		expected          json.RawMessage
		actual            json.RawMessage
		err               error
		useRelativeError  bool
		skipRecentSamples time.Duration
	}{
		{
			name: "difference in response status",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"scalar","result":[1,"1"]}
						}`),
			actual: json.RawMessage(`{
							"status": "fail"
						}`),
			err: errors.New("expected status success but got fail"),
		},
		{
			name: "difference in resultType",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"scalar","result":[1,"1"]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"1"]}]}
						}`),
			err: errors.New("expected resultType scalar but got vector"),
		},
		{
			name: "unregistered resultType",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"new-scalar","result":[1,"1"]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"new-scalar","result":[1,"1"]}
						}`),
			err: errors.New("resultType new-scalar not registered for comparison"),
		},
		{
			name: "valid scalar response",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"scalar","result":[1,"1"]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"scalar","result":[1,"1"]}
						}`),
		},
		{
			name:      "should pass if values are slightly different but within the tolerance",
			tolerance: 0.000001,
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"773054.5916666666"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"773054.59166667"]}]}
						}`),
		},
		{
			name:      "should correctly compare NaN values with tolerance is disabled",
			tolerance: 0,
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"NaN"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"NaN"]}]}
						}`),
		},
		{
			name:      "should correctly compare NaN values with tolerance is enabled",
			tolerance: 0.000001,
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"NaN"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"NaN"]}]}
						}`),
		},
		{
			name: "should correctly compare Inf values",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"Inf"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"Inf"]}]}
						}`),
		},
		{
			name: "should correctly compare -Inf values",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"-Inf"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"-Inf"]}]}
						}`),
		},
		{
			name: "should correctly compare +Inf values",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"+Inf"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"+Inf"]}]}
						}`),
		},
		{
			name:      "should fail if values are significantly different, over the tolerance",
			tolerance: 0.000001,
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"773054.5916666666"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"773054.789"]}]}
						}`),
			err: errors.New(`sample pair not matching for metric {foo="bar"}: expected value 773054.5916666666 for timestamp 1 but got 773054.789`),
		},
		{
			name:      "should fail if large values are significantly different, over the tolerance without using relative error",
			tolerance: 1e-14,
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"4.923488536785282e+41"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"4.923488536785281e+41"]}]}
						}`),
			err: errors.New(`sample pair not matching for metric {foo="bar"}: expected value 492348853678528200000000000000000000000000 for timestamp 1 but got 492348853678528100000000000000000000000000`),
		},
		{
			name:      "should not fail if large values are significantly different, over the tolerance using relative error",
			tolerance: 1e-14,
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"4.923488536785282e+41"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[1,"4.923488536785281e+41"]}]}
						}`),
			useRelativeError: true,
		},
		{
			name: "should not fail when the sample is recent and configured to skip",
			expected: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[` + now + `,"10"]}]}
						}`),
			actual: json.RawMessage(`{
							"status": "success",
							"data": {"resultType":"vector","result":[{"metric":{"foo":"bar"},"value":[` + now + `,"5"]}]}
						}`),
			skipRecentSamples: time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samplesComparator := NewSamplesComparator(SampleComparisonOptions{
				Tolerance:         tc.tolerance,
				UseRelativeError:  tc.useRelativeError,
				SkipRecentSamples: tc.skipRecentSamples,
			})
			_, err := samplesComparator.Compare(tc.expected, tc.actual)
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.err.Error(), err.Error())
		})
	}
}

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
			_, err := compareStreams(tc.expected, tc.actual, SampleComparisonOptions{Tolerance: 0})
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.err.Error(), err.Error())
		})
	}
}
