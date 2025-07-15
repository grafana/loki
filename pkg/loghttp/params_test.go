package loghttp

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
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

func Test_determineBounds(t *testing.T) {
	type args struct {
		now         time.Time
		startString string
		endString   string
		sinceString string
	}
	tests := []struct {
		name    string
		args    args
		start   time.Time
		end     time.Time
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "no start, end, since",
			args: args{
				now:         time.Unix(3600, 0),
				startString: "",
				endString:   "",
				sinceString: "",
			},
			start:   time.Unix(0, 0),    // Default start is one hour before 'now' if nothing is provided
			end:     time.Unix(3600, 0), // Default end is 'now' if nothing is provided
			wantErr: assert.NoError,
		},
		{
			name: "no since or no start with end in the future",
			args: args{
				now:         time.Unix(3600, 0),
				startString: "",
				endString:   "2022-12-18T00:00:00Z",
				sinceString: "",
			},
			start:   time.Unix(0, 0), // Default should be one hour before now
			end:     time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
			wantErr: assert.NoError,
		},
		{
			name: "no since, valid start and end",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "2022-12-17T00:00:00Z",
				endString:   "2022-12-18T00:00:00Z",
				sinceString: "",
			},
			start:   time.Date(2022, 12, 17, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
			wantErr: assert.NoError,
		},
		{
			name: "invalid end",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "2022-12-17T00:00:00Z",
				endString:   "WHAT TIME IS IT?",
				sinceString: "",
			},
			start: time.Time{},
			end:   time.Time{},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "could not parse 'end' parameter:", i...)
			},
		},
		{
			name: "invalid start",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "LET'S GOOO",
				endString:   "2022-12-18T00:00:00Z",
				sinceString: "",
			},
			start: time.Time{},
			end:   time.Time{},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "could not parse 'start' parameter:", i...)
			},
		},
		{
			name: "invalid since",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "2022-12-17T00:00:00Z",
				endString:   "2022-12-18T00:00:00Z",
				sinceString: "HI!",
			},
			start: time.Time{},
			end:   time.Time{},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "could not parse 'since' parameter:", i...)
			},
		},
		{
			name: "since 1h with no start or end",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "",
				endString:   "",
				sinceString: "1h",
			},
			start:   time.Date(2022, 12, 17, 23, 0, 0, 0, time.UTC),
			end:     time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
			wantErr: assert.NoError,
		},
		{
			name: "since 1d with no start or end",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "",
				endString:   "",
				sinceString: "1d",
			},
			start:   time.Date(2022, 12, 17, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
			wantErr: assert.NoError,
		},
		{
			name: "since 1h with no start and end time in the past",
			args: args{
				now:         time.Date(2022, 12, 18, 0, 0, 0, 0, time.UTC),
				startString: "",
				endString:   "2022-12-17T00:00:00Z",
				sinceString: "1h",
			},
			start:   time.Date(2022, 12, 16, 23, 0, 0, 0, time.UTC), // start should be calculated relative to end when end is specified
			end:     time.Date(2022, 12, 17, 0, 0, 0, 0, time.UTC),
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := determineBounds(tt.args.now, tt.args.startString, tt.args.endString, tt.args.sinceString)
			if !tt.wantErr(t, err, fmt.Sprintf("determineBounds(%v, %v, %v, %v)", tt.args.now, tt.args.startString, tt.args.endString, tt.args.sinceString)) {
				return
			}
			assert.Equalf(t, tt.start, got, "determineBounds(%v, %v, %v, %v)", tt.args.now, tt.args.startString, tt.args.endString, tt.args.sinceString)
			assert.Equalf(t, tt.end, got1, "determineBounds(%v, %v, %v, %v)", tt.args.now, tt.args.startString, tt.args.endString, tt.args.sinceString)
		})
	}
}
