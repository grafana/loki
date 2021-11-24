package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPrepopulate(t *testing.T) {
	success := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, err := w.Write([]byte("ok"))
		require.Nil(t, err)
	})

	for _, tc := range []struct {
		desc     string
		method   string
		qs       string
		body     io.Reader
		expected url.Values
		error    bool
	}{
		{
			desc:   "passthrough GET w/ querystring",
			method: "GET",
			qs:     "?" + url.Values{"foo": {"bar"}}.Encode(),
			body:   nil,
			expected: url.Values{
				"foo": {"bar"},
			},
		},
		{
			desc:   "passthrough POST w/ querystring",
			method: "POST",
			qs:     "?" + url.Values{"foo": {"bar"}}.Encode(),
			body:   nil,
			expected: url.Values{
				"foo": {"bar"},
			},
		},
		{
			desc:   "parse form body",
			method: "POST",
			qs:     "",
			body: bytes.NewBuffer([]byte(url.Values{
				"match": {"up", "down"},
			}.Encode())),
			expected: url.Values{
				"match": {"up", "down"},
			},
		},
		{
			desc:   "querystring extends form body",
			method: "POST",
			qs: "?" + url.Values{
				"match": {"sideways"},
				"foo":   {"bar"},
			}.Encode(),
			body: bytes.NewBuffer([]byte(url.Values{
				"match": {"up", "down"},
			}.Encode())),
			expected: url.Values{
				"match": {"up", "down", "sideways"},
				"foo":   {"bar"},
			},
		},
		{
			desc:   "nil body",
			method: "POST",
			body:   nil,
			error:  true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://testing"+tc.qs, tc.body)

			// For some reason nil bodies aren't maintained after passed to httptest.NewRequest,
			// but are a failure condition for parsing the form data.
			// Therefore set to nil if we're passing a nil body to force an error.
			if tc.body == nil {
				req.Body = nil
			}

			if tc.method == "POST" {
				req.Header["Content-Type"] = []string{"application/x-www-form-urlencoded"}
			}

			w := httptest.NewRecorder()
			mware := NewPrepopulateMiddleware().Wrap(success)

			mware.ServeHTTP(w, req)

			if tc.error {
				require.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
			} else {
				require.Equal(t, tc.expected, req.Form)
			}

		})
	}
}
