package server

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logql"
)

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
			WriteError(tt.err, rec)
			require.Equal(t, tt.expectedStatus, rec.Result().StatusCode)
			b, err := ioutil.ReadAll(rec.Result().Body)
			if err != nil {
				t.Fatal(err)
			}
			require.Equal(t, tt.msg, string(b[:len(b)-1]))
		})
	}
}
