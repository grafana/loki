package loghttp

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
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
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
