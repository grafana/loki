package httpreq

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
			exp:  `Source=logvolhist,Statate=beta`,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://testing.com", nil)
			req.Header.Set(string(QueryTagsHTTPHeader), tc.in)

			w := httptest.NewRecorder()
			checked := false
			mware := ExtractQueryTagsMiddleware().Wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				require.Equal(t, tc.exp, req.Context().Value(QueryTagsHTTPHeader).(string))
				checked = true
			}))

			mware.ServeHTTP(w, req)

			assert.True(t, true, checked)
		})
	}
}
