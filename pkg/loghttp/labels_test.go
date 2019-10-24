package loghttp

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/grafana/loki/pkg/logproto"
)

func TestParseLabelQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		r       *http.Request
		want    *logproto.LabelRequest
		wantErr bool
	}{
		{"bad start", &http.Request{URL: mustParseURL(`?&start=t`)}, nil, true},
		{"bad end", &http.Request{URL: mustParseURL(`?&start=2016-06-10T21:42:24.760738998Z&end=h`)}, nil, true},
		{"good no name in the pah",
			requestWithVar(&http.Request{
				URL: mustParseURL(`?start=2017-06-10T21:42:24.760738998Z&end=2017-07-10T21:42:24.760738998Z`),
			}, "name", "test"), &logproto.LabelRequest{
				Name:   "test",
				Values: true,
				Start:  timePtr(time.Date(2017, 06, 10, 21, 42, 24, 760738998, time.UTC)),
				End:    timePtr(time.Date(2017, 07, 10, 21, 42, 24, 760738998, time.UTC)),
			}, false},
		{"good with name",
			&http.Request{
				URL: mustParseURL(`?start=2017-06-10T21:42:24.760738998Z&end=2017-07-10T21:42:24.760738998Z`),
			}, &logproto.LabelRequest{
				Name:   "",
				Values: false,
				Start:  timePtr(time.Date(2017, 06, 10, 21, 42, 24, 760738998, time.UTC)),
				End:    timePtr(time.Date(2017, 07, 10, 21, 42, 24, 760738998, time.UTC)),
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLabelQuery(tt.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLabelQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLabelQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func requestWithVar(req *http.Request, name, value string) *http.Request {
	return mux.SetURLVars(req, map[string]string{
		name: value,
	})
}
