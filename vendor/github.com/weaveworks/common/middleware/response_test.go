package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBadResponseLoggingWriter(t *testing.T) {
	for _, tc := range []struct {
		statusCode int
		data       string
		expected   string
	}{
		{http.StatusOK, "", ""},
		{http.StatusOK, "some data", ""},
		{http.StatusUnprocessableEntity, "unprocessable", ""},
		{http.StatusInternalServerError, "", ""},
		{http.StatusInternalServerError, "bad juju", "bad juju\n"},
	} {
		w := httptest.NewRecorder()
		var buf bytes.Buffer
		wrapped := newBadResponseLoggingWriter(w, &buf)
		switch {
		case tc.data == "":
			wrapped.WriteHeader(tc.statusCode)
		case tc.statusCode < 300 && tc.data != "":
			wrapped.WriteHeader(tc.statusCode)
			wrapped.Write([]byte(tc.data))
		default:
			http.Error(wrapped, tc.data, tc.statusCode)
		}
		if wrapped.statusCode != tc.statusCode {
			t.Errorf("Wrong status code: have %d want %d", wrapped.statusCode, tc.statusCode)
		}
		data := string(buf.Bytes())
		if data != tc.expected {
			t.Errorf("Wrong data: have %q want %q", data, tc.expected)
		}
	}
}
