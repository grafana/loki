package nethttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func makeRequest(t *testing.T, url string, options ...ClientOption) []*mocktracer.MockSpan {
	tr := &mocktracer.MockTracer{}
	span := tr.StartSpan("toplevel")
	client := &http.Client{Transport: &Transport{}}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(opentracing.ContextWithSpan(req.Context(), span))
	req, ht := TraceRequest(tr, req, options...)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	ht.Finish()
	span.Finish()

	return tr.FinishedSpans()
}

func TestClientTrace(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failure", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tests := []struct {
		url    string
		num    int
		opts   []ClientOption
		opName string
	}{
		{url: "/ok", num: 3, opts: nil, opName: "HTTP Client"},
		{url: "/redirect", num: 4, opts: []ClientOption{OperationName("client-span")}, opName: "client-span"},
		{url: "/fail", num: 3, opts: nil, opName: "HTTP Client"},
	}

	for _, tt := range tests {
		t.Log(tt.opName)
		spans := makeRequest(t, srv.URL+tt.url, tt.opts...)
		if got, want := len(spans), tt.num; got != want {
			t.Fatalf("got %d spans, expected %d", got, want)
		}
		var rootSpan *mocktracer.MockSpan
		for _, span := range spans {
			if span.ParentID == 0 {
				rootSpan = span
				break
			}
		}
		if rootSpan == nil {
			t.Fatal("cannot find root span with ParentID==0")
		}

		foundClientSpan := false
		for _, span := range spans {
			if span.ParentID == rootSpan.SpanContext.SpanID {
				foundClientSpan = true
				if got, want := span.OperationName, tt.opName; got != want {
					t.Fatalf("got %s operation name, expected %s", got, want)
				}
			}
			if span.OperationName == "HTTP GET" {
				logs := span.Logs()
				if len(logs) < 6 {
					t.Fatalf("got %d expected at least %d log events", len(logs), 6)
				}

				key := logs[0].Fields[0].Key
				if key != "event" {
					t.Fatalf("got %s expected, %s", key, "event")
				}
				v := logs[0].Fields[0].ValueString
				if v != "GetConn" {
					t.Fatalf("got %s expected, %s", v, "GetConn")
				}
			}
		}
		if !foundClientSpan {
			t.Fatal("cannot find client span")
		}
	}
}
