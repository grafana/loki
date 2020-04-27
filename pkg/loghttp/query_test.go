package loghttp

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestParseRangeQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		r       *http.Request
		want    *RangeQuery
		wantErr bool
	}{
		{"bad start", &http.Request{URL: mustParseURL(`?query={foo="bar"}&start=t`)}, nil, true},
		{"bad end", &http.Request{URL: mustParseURL(`?query={foo="bar"}&end=t`)}, nil, true},
		{"end before start", &http.Request{URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2015-06-10T21:42:24.760738998Z`)}, nil, true},
		{"end equal start", &http.Request{URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2016-06-10T21:42:24.760738998Z`)}, nil, true},
		{"bad limit", &http.Request{URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2017-06-10T21:42:24.760738998Z&limit=h`)}, nil, true},
		{"bad direction",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2017-06-10T21:42:24.760738998Z&limit=100&direction=fw`),
			}, nil, true},
		{"bad step",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2017-06-10T21:42:24.760738998Z&limit=100&direction=FORWARD&step=h`),
			}, nil, true},
		{"negative step",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2017-06-10T21:42:24.760738998Z&limit=100&direction=BACKWARD&step=-1`),
			}, nil, true},
		{"too small step",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2017-06-10T21:42:24.760738998Z&limit=100&direction=BACKWARD&step=1`),
			}, nil, true},
		{"negative interval",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2016-06-10T21:42:24.760738998Z&end=2017-06-10T21:42:24.760738998Z&limit=100&direction=BACKWARD&step=1,interval=-1`),
			}, nil, true},
		{"good",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&start=2017-06-10T21:42:24.760738998Z&end=2017-07-10T21:42:24.760738998Z&limit=1000&direction=BACKWARD&step=3600`),
			}, &RangeQuery{
				Step:      time.Hour,
				Query:     `{foo="bar"}`,
				Direction: logproto.BACKWARD,
				Start:     time.Date(2017, 06, 10, 21, 42, 24, 760738998, time.UTC),
				End:       time.Date(2017, 07, 10, 21, 42, 24, 760738998, time.UTC),
				Limit:     1000,
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.r.ParseForm()
			require.Nil(t, err)

			got, err := ParseRangeQuery(tt.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRangeQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseRangeQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseInstantQuery(t *testing.T) {

	tests := []struct {
		name    string
		r       *http.Request
		want    *InstantQuery
		wantErr bool
	}{
		{"bad time", &http.Request{URL: mustParseURL(`?query={foo="bar"}&time=t`)}, nil, true},
		{"bad limit", &http.Request{URL: mustParseURL(`?query={foo="bar"}&time=2016-06-10T21:42:24.760738998Z&limit=h`)}, nil, true},
		{"bad direction",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&time=2016-06-10T21:42:24.760738998Z&limit=100&direction=fw`),
			}, nil, true},
		{"good",
			&http.Request{
				URL: mustParseURL(`?query={foo="bar"}&time=2017-06-10T21:42:24.760738998Z&limit=1000&direction=BACKWARD`),
			}, &InstantQuery{
				Query:     `{foo="bar"}`,
				Direction: logproto.BACKWARD,
				Ts:        time.Date(2017, 06, 10, 21, 42, 24, 760738998, time.UTC),
				Limit:     1000,
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.r.ParseForm()
			require.Nil(t, err)
			got, err := ParseInstantQuery(tt.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseInstantQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseInstantQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustParseURL(u string) *url.URL {
	url, err := url.Parse(u)
	if err != nil {
		panic(err)
	}
	return url
}

func TestStreams_ToProto(t *testing.T) {
	tests := []struct {
		name string
		s    Streams
		want []logproto.Stream
	}{
		{"empty", nil, nil},
		{"some", []Stream{
			{
				Labels: map[string]string{"foo": "bar"},
				Entries: []Entry{
					{Timestamp: time.Unix(0, 1), Line: "1"},
					{Timestamp: time.Unix(0, 2), Line: "2"},
				},
			},
			{
				Labels: map[string]string{"foo": "bar", "lvl": "error"},
				Entries: []Entry{
					{Timestamp: time.Unix(0, 3), Line: "3"},
					{Timestamp: time.Unix(0, 4), Line: "4"},
				},
			},
		},
			[]logproto.Stream{
				{
					Labels: `{foo="bar"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
						{Timestamp: time.Unix(0, 2), Line: "2"},
					},
				},
				{
					Labels: `{foo="bar", lvl="error"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 3), Line: "3"},
						{Timestamp: time.Unix(0, 4), Line: "4"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.ToProto(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Streams.ToProto() = %v, want %v", got, tt.want)
			}
		})
	}
}
