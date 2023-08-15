package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestWriteError(t *testing.T) {
	for _, test := range []struct {
		status int
		err    error
	}{
		{http.StatusInternalServerError, errors.New("unknown")},
		{http.StatusGatewayTimeout, context.DeadlineExceeded},
		{StatusClientClosedRequest, context.Canceled},
		{http.StatusBadRequest, httpgrpc.Errorf(http.StatusBadRequest, "")},
	} {
		t.Run(test.err.Error(), func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, test.err)
			require.Equal(t, test.status, w.Result().StatusCode)
		})
	}
}
