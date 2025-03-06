package httpreq

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryTags(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		in    string
		exp   string
		error bool
	}{
		{
			desc: "single-value",
			in:   `Source=logvolhist`,
			exp:  `Source=logvolhist`,
		},
		{
			desc: "multiple-values",
			in:   `Source=logvolhist,Statate=beta`,
			exp:  `Source=logvolhist,Statate=beta`,
		},
		{
			desc: "remove-invalid-chars",
			in:   `Source=log+volhi\\st,Statate=be$ta`,
			exp:  `Source=log_volhi_st,Statate=be_ta`,
		},
		{
			desc: "test invalid char set",
			in:   `Source=abc.def@geh.com_test-test`,
			exp:  `Source=abc.def@geh.com_test-test`,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://testing.com", nil)
			req.Header.Set(string(QueryTagsHTTPHeader), tc.in)

			w := httptest.NewRecorder()
			checked := false
			mware := ExtractQueryTagsMiddleware().Wrap(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				require.Equal(t, tc.exp, req.Context().Value(QueryTagsHTTPHeader).(string))
				checked = true
			}))

			mware.ServeHTTP(w, req)

			assert.True(t, true, checked)
		})
	}
}

func TestQueryMetrics(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		in    string
		exp   interface{}
		error bool
	}{
		{
			desc: "valid time duration",
			in:   `2s`,
			exp:  2 * time.Second,
		},
		{
			desc: "empty header",
			in:   ``,
			exp:  nil,
		},
		{
			desc: "invalid time duration",
			in:   `foo`,
			exp:  nil,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://testing.com", nil)
			req.Header.Set(string(QueryQueueTimeHTTPHeader), tc.in)

			w := httptest.NewRecorder()
			checked := false
			mware := ExtractQueryMetricsMiddleware().Wrap(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				require.Equal(t, tc.exp, req.Context().Value(QueryQueueTimeHTTPHeader))
				checked = true
			}))

			mware.ServeHTTP(w, req)

			assert.True(t, true, checked)
		})
	}
}

func Test_testToKeyValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		exp  []interface{}
	}{
		{
			name: "canonical-form",
			in:   "Source=logvolhist",
			exp: []interface{}{
				"source",
				"logvolhist",
			},
		},
		{
			name: "canonical-form-multiple-values",
			in:   "Source=logvolhist,Feature=beta,User=Jinx@grafana.com",
			exp: []interface{}{
				"source",
				"logvolhist",
				"feature",
				"beta",
				"user",
				"Jinx@grafana.com",
			},
		},
		{
			name: "empty",
			in:   "",
			exp:  []interface{}{},
		},
		{
			name: "non-canonical form",
			in:   "abc",
			exp:  []interface{}{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TagsToKeyValues(c.in)
			assert.Equal(t, c.exp, got)
		})
	}
}
