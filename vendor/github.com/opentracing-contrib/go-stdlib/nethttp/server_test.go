package nethttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func TestOperationNameOption(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/root", func(w http.ResponseWriter, r *http.Request) {})

	fn := func(r *http.Request) string {
		return "HTTP " + r.Method + ": /root"
	}

	tests := []struct {
		options []MWOption
		opName  string
	}{
		{nil, "HTTP GET"},
		{[]MWOption{OperationNameFunc(fn)}, "HTTP GET: /root"},
	}

	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.opName, func(t *testing.T) {
			tr := &mocktracer.MockTracer{}
			mw := Middleware(tr, mux, testCase.options...)
			srv := httptest.NewServer(mw)
			defer srv.Close()

			_, err := http.Get(srv.URL)
			if err != nil {
				t.Fatalf("server returned error: %v", err)
			}

			spans := tr.FinishedSpans()
			if got, want := len(spans), 1; got != want {
				t.Fatalf("got %d spans, expected %d", got, want)
			}

			if got, want := spans[0].OperationName, testCase.opName; got != want {
				t.Fatalf("got %s operation name, expected %s", got, want)
			}
		})
	}
}

func TestSpanObserverOption(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/root", func(w http.ResponseWriter, r *http.Request) {})

	opNamefn := func(r *http.Request) string {
		return "HTTP " + r.Method + ": /root"
	}
	spanObserverfn := func(sp opentracing.Span, r *http.Request) {
		sp.SetTag("http.uri", r.URL.EscapedPath())
	}
	wantTags := map[string]interface{}{"http.uri": "/"}

	tests := []struct {
		options []MWOption
		opName  string
		Tags    map[string]interface{}
	}{
		{nil, "HTTP GET", nil},
		{[]MWOption{OperationNameFunc(opNamefn)}, "HTTP GET: /root", nil},
		{[]MWOption{MWSpanObserver(spanObserverfn)}, "HTTP GET", wantTags},
		{[]MWOption{OperationNameFunc(opNamefn), MWSpanObserver(spanObserverfn)}, "HTTP GET: /root", wantTags},
	}

	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.opName, func(t *testing.T) {
			tr := &mocktracer.MockTracer{}
			mw := Middleware(tr, mux, testCase.options...)
			srv := httptest.NewServer(mw)
			defer srv.Close()

			_, err := http.Get(srv.URL)
			if err != nil {
				t.Fatalf("server returned error: %v", err)
			}

			spans := tr.FinishedSpans()
			if got, want := len(spans), 1; got != want {
				t.Fatalf("got %d spans, expected %d", got, want)
			}

			if got, want := spans[0].OperationName, testCase.opName; got != want {
				t.Fatalf("got %s operation name, expected %s", got, want)
			}

			defaultLength := 5
			if len(spans[0].Tags()) != len(testCase.Tags)+defaultLength {
				t.Fatalf("got tag length %d, expected %d", len(spans[0].Tags()), len(testCase.Tags))
			}
			for k, v := range testCase.Tags {
				if tag := spans[0].Tag(k); v != tag.(string) {
					t.Fatalf("got %v tag, expected %v", tag, v)
				}
			}
		})
	}
}

func TestSpanFilterOption(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/root", func(w http.ResponseWriter, r *http.Request) {})

	spanFilterfn := func(r *http.Request) bool {
		return !strings.HasPrefix(r.Header.Get("User-Agent"), "kube-probe")
	}
	noAgentReq, _ := http.NewRequest("GET", "/root", nil)
	noAgentReq.Header.Del("User-Agent")
	probeReq1, _ := http.NewRequest("GET", "/root", nil)
	probeReq1.Header.Add("User-Agent", "kube-probe/1.12")
	probeReq2, _ := http.NewRequest("GET", "/root", nil)
	probeReq2.Header.Add("User-Agent", "kube-probe/9.99")
	postmanReq, _ := http.NewRequest("GET", "/root", nil)
	postmanReq.Header.Add("User-Agent", "PostmanRuntime/7.3.0")
	tests := []struct {
		options            []MWOption
		request            *http.Request
		opName             string
		ExpectToCreateSpan bool
	}{
		{nil, noAgentReq, "No filter", true},
		{[]MWOption{MWSpanFilter(spanFilterfn)}, noAgentReq, "No User-Agent", true},
		{[]MWOption{MWSpanFilter(spanFilterfn)}, probeReq1, "User-Agent: kube-probe/1.12", false},
		{[]MWOption{MWSpanFilter(spanFilterfn)}, probeReq2, "User-Agent: kube-probe/9.99", false},
		{[]MWOption{MWSpanFilter(spanFilterfn)}, postmanReq, "User-Agent: PostmanRuntime/7.3.0", true},
	}

	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.opName, func(t *testing.T) {
			tr := &mocktracer.MockTracer{}
			mw := Middleware(tr, mux, testCase.options...)
			srv := httptest.NewServer(mw)
			defer srv.Close()

			client := &http.Client{}
			testCase.request.URL, _ = url.Parse(srv.URL)
			_, err := client.Do(testCase.request)
			if err != nil {
				t.Fatalf("server returned error: %v", err)
			}

			spans := tr.FinishedSpans()
			if spanCreated := len(spans) == 1; spanCreated != testCase.ExpectToCreateSpan {
				t.Fatalf("spanCreated %t, ExpectToCreateSpan %t", spanCreated, testCase.ExpectToCreateSpan)
			}
		})
	}
}

func BenchmarkStatusCodeTrackingOverhead(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("/root", func(w http.ResponseWriter, r *http.Request) {})
	tr := &mocktracer.MockTracer{}
	mw := Middleware(tr, mux)
	srv := httptest.NewServer(mw)
	defer srv.Close()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(srv.URL)
			if err != nil {
				b.Fatalf("server returned error: %v", err)
			}
			err = resp.Body.Close()
			if err != nil {
				b.Fatalf("failed to close response: %v", err)
			}
		}
	})
}
