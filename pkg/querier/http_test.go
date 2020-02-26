package querier

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logql"
)

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

func Test_writeError(t *testing.T) {
	for _, tt := range []struct {
		name string

		err            error
		msg            string
		expectedStatus int
	}{
		{"cancelled", context.Canceled, context.Canceled.Error(), StatusClientClosedRequest},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded.Error(), http.StatusGatewayTimeout},
		{"parse error", logql.ParseError{}, "parse error : ", http.StatusBadRequest},
		{"httpgrpc", httpgrpc.Errorf(http.StatusBadRequest, errors.New("foo").Error()), "foo", http.StatusBadRequest},
		{"internal", errors.New("foo"), "foo", http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(tt.err, rec)
			require.Equal(t, tt.expectedStatus, rec.Result().StatusCode)
			b, err := ioutil.ReadAll(rec.Result().Body)
			if err != nil {
				t.Fatal(err)
			}
			require.Equal(t, tt.msg, string(b[:len(b)-1]))
		})
	}
}
