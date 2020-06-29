package loghttp

import (
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestHttp_defaultQueryRangeStep(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		start    time.Time
		end      time.Time
		expected int
	}{
		"should not be lower then 1s": {
			start:    time.Unix(60, 0),
			end:      time.Unix(60, 0),
			expected: 1,
		},
		"should return 1s if input time range is 5m": {
			start:    time.Unix(60, 0),
			end:      time.Unix(360, 0),
			expected: 1,
		},
		"should return 14s if input time range is 1h": {
			start:    time.Unix(60, 0),
			end:      time.Unix(3660, 0),
			expected: 14,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			assert.Equal(t, testData.expected, defaultQueryRangeStep(testData.start, testData.end))
		})
	}
}

func TestHttp_ParseRangeQuery_Step(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		reqPath  string
		expected *RangeQuery
	}{
		"should set the default step based on the input time range if the step parameter is not provided": {
			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000",
			expected: &RangeQuery{
				Query:     "{}",
				Start:     time.Unix(0, 0),
				End:       time.Unix(3600, 0),
				Step:      14 * time.Second,
				Limit:     100,
				Direction: logproto.BACKWARD,
			},
		},
		"should use the input step parameter if provided as an integer": {
			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000&step=5",
			expected: &RangeQuery{
				Query:     "{}",
				Start:     time.Unix(0, 0),
				End:       time.Unix(3600, 0),
				Step:      5 * time.Second,
				Limit:     100,
				Direction: logproto.BACKWARD,
			},
		},
		"should use the input step parameter if provided as a float without decimals": {
			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000&step=5.000",
			expected: &RangeQuery{
				Query:     "{}",
				Start:     time.Unix(0, 0),
				End:       time.Unix(3600, 0),
				Step:      5 * time.Second,
				Limit:     100,
				Direction: logproto.BACKWARD,
			},
		},
		"should use the input step parameter if provided as a float with decimals": {
			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000&step=5.500",
			expected: &RangeQuery{
				Query:     "{}",
				Start:     time.Unix(0, 0),
				End:       time.Unix(3600, 0),
				Step:      5.5 * 1e9,
				Limit:     100,
				Direction: logproto.BACKWARD,
			},
		},
		"should use the input step parameter if provided as a duration in seconds": {
			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000&step=5s",
			expected: &RangeQuery{
				Query:     "{}",
				Start:     time.Unix(0, 0),
				End:       time.Unix(3600, 0),
				Step:      5 * time.Second,
				Limit:     100,
				Direction: logproto.BACKWARD,
			},
		},
		"should use the input step parameter if provided as a duration in days": {
			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000&step=5d",
			expected: &RangeQuery{
				Query:     "{}",
				Start:     time.Unix(0, 0),
				End:       time.Unix(3600, 0),
				Step:      5 * 24 * 3600 * time.Second,
				Limit:     100,
				Direction: logproto.BACKWARD,
			},
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			req := httptest.NewRequest("GET", testData.reqPath, nil)
			err := req.ParseForm()
			require.Nil(t, err)
			actual, err := ParseRangeQuery(req)

			require.NoError(t, err)
			assert.Equal(t, testData.expected, actual)
		})
	}
}

func Test_interval(t *testing.T) {
	tests := []struct {
		name     string
		reqPath  string
		expected time.Duration
		wantErr  bool
	}{
		{
			"valid_step_int",
			"/loki/api/v1/query_range?query={}&start=0&end=3600000000000&interval=5",
			5 * time.Second,
			false,
		},
		{
			"valid_step_duration",
			"/loki/api/v1/query_range?query={}&start=0&end=3600000000000&interval=5m",
			5 * time.Minute,
			false,
		},
		{
			"invalid",
			"/loki/api/v1/query_range?query={}&start=0&end=3600000000000&interval=a",
			0,
			true,
		},
		{
			"valid_0",
			"/loki/api/v1/query_range?query={}&start=0&end=3600000000000&interval=0",
			0,
			false,
		},
		{
			"valid_not_included",
			"/loki/api/v1/query_range?query={}&start=0&end=3600000000000",
			0,
			false,
		},
	}
	for _, testData := range tests {
		testData := testData

		t.Run(testData.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", testData.reqPath, nil)
			err := req.ParseForm()
			require.Nil(t, err)
			actual, err := interval(req)
			if testData.wantErr {
				require.Error(t, err)
			} else {
				assert.Equal(t, testData.expected, actual)
			}
		})
	}
}

func Test_parseTimestamp(t *testing.T) {

	now := time.Now()

	tests := []struct {
		name    string
		value   string
		def     time.Time
		want    time.Time
		wantErr bool
	}{
		{"default", "", now, now, false},
		{"unix timestamp", "1571332130", now, time.Unix(1571332130, 0), false},
		{"unix nano timestamp", "1571334162051000000", now, time.Unix(0, 1571334162051000000), false},
		{"unix timestamp with subseconds", "1571332130.934", now, time.Unix(1571332130, 934*1e6), false},
		{"RFC3339 format", "2002-10-02T15:00:00Z", now, time.Date(2002, 10, 02, 15, 0, 0, 0, time.UTC), false},
		{"RFC3339nano format", "2009-11-10T23:00:00.000000001Z", now, time.Date(2009, 11, 10, 23, 0, 0, 1, time.UTC), false},
		{"invalid", "we", now, time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimestamp(tt.value, tt.def)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_match(t *testing.T) {

	tests := []struct {
		name    string
		input   []string
		want    [][]*labels.Matcher
		wantErr bool
	}{
		{"malformed", []string{`{a="1`}, nil, true},
		{"empty on nil input", nil, [][]*labels.Matcher{}, false},
		{"empty on empty input", []string{}, [][]*labels.Matcher{}, false},
		{
			"single",
			[]string{`{a="1"}`},
			[][]*labels.Matcher{
				{mustMatcher(labels.MatchEqual, "a", "1")},
			},
			false,
		},
		{
			"multiple groups",
			[]string{`{a="1"}`, `{b="2", c=~"3", d!="4"}`},
			[][]*labels.Matcher{
				{mustMatcher(labels.MatchEqual, "a", "1")},
				{
					mustMatcher(labels.MatchEqual, "b", "2"),
					mustMatcher(labels.MatchRegexp, "c", "3"),
					mustMatcher(labels.MatchNotEqual, "d", "4"),
				},
			},
			false,
		},
		{
			"errors on empty group",
			[]string{`{}`},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Match(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.Equal(t, tt.want, got)
			}

		})
	}
}

func mustMatcher(t labels.MatchType, n string, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}
