package loghttp

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
)

func TestParseTailQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		r       *http.Request
		want    *logproto.TailRequest
		wantErr bool
	}{
		{"bad time", &http.Request{URL: mustParseURL(`?query={foo="bar"}&start=t`)}, nil, true},
		{"bad limit", &http.Request{URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&limit=h`)}, nil, true},
		{"bad delay",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&time=2016-06-10T21:42:24.760738998Z&limit=100&delay_for=fw`),
			}, nil, true},
		{"too much delay",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&time=2016-06-10T21:42:24.760738998Z&limit=100&delay_for=20`),
			}, nil, true},
		{"good",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2017-06-10T21:42:24.760738998Z&limit=1000&delay_for=5`),
			}, &logproto.TailRequest{
				Query:    `{foo="bar"}`,
				DelayFor: 5,
				Start:    time.Date(2017, 06, 10, 21, 42, 24, 760738998, time.UTC),
				Limit:    1000,
				Plan:     testutil.MustPlan(`{foo="bar"}`),
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.r.ParseForm()
			require.Nil(t, err)
			got, err := ParseTailQuery(tt.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTailQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseTailQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDroppedStreamJSON(t *testing.T) {
	for name, tc := range map[string]struct {
		in       DroppedStream
		expected string
	}{
		"with labels": {
			in:       DroppedStream{Timestamp: time.Unix(0, 42), Labels: LabelSet{"b": "2", "a": "1\n"}},
			expected: `{"timestamp":"42","labels":{"a":"1\n","b":"2"}}`,
		},
		"no labels": {
			in:       DroppedStream{Timestamp: time.Unix(0, 42)},
			expected: `{"timestamp":"42"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(&tc.in)
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, string(data))

			var out DroppedStream
			require.NoError(t, json.Unmarshal(data, &out))
			require.Equal(t, tc.in.Timestamp, out.Timestamp)
			require.Equal(t, tc.in.Labels, out.Labels)
		})
	}

	var out DroppedStream
	require.Error(t, json.Unmarshal([]byte(`{"timestamp":42}`), &out))
	require.Error(t, json.Unmarshal([]byte(`{"labels":{"a":"b"}}`), &out))
	require.Nil(t, out.Labels)
}
