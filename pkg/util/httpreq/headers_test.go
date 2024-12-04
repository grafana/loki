package httpreq

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryNoSplit(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		in    string
		exp   bool
		error bool
	}{
		{
			desc: "true",
			in:   `true`,
			exp:  true,
		},
		{
			desc: "false",
			in:   `false`,
			exp:  false,
		},
		{
			desc: "absent",
			in:   ``,
			exp:  false,
		},
		{
			desc: "garbage",
			in:   `garbage`,
			exp:  false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://testing.com", nil)
			if tc.in != "" {
				req.Header.Set(QueryNoSplitHTTPHeader, tc.in)
			}

			w := httptest.NewRecorder()
			checked := false
			mware := ExtractQueryNoSplitMiddleware().Wrap(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				noSplit := req.Context().Value(QueryNoSplitHTTPHeader)
				noSplitBool, ok := noSplit.(bool)
				require.True(t, ok)
				require.Equal(t, tc.exp, noSplitBool)
				checked = true
			}))

			mware.ServeHTTP(w, req)

			assert.True(t, true, checked)
		})
	}
}
