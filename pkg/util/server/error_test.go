package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
	storage_errors "github.com/grafana/loki/pkg/storage/errors"
	"github.com/grafana/loki/pkg/util"
)

func Test_writeError(t *testing.T) {
	for _, tt := range []struct {
		name string

		err            error
		msg            string
		expectedStatus int
	}{
		{"cancelled", context.Canceled, ErrClientCanceled, StatusClientClosedRequest},
		{"cancelled multi", util.MultiError{context.Canceled, context.Canceled}, ErrClientCanceled, StatusClientClosedRequest},
		{"rpc cancelled", status.New(codes.Canceled, context.Canceled.Error()).Err(), "rpc error: code = Canceled desc = context canceled", http.StatusInternalServerError},
		{"rpc cancelled multi", util.MultiError{status.New(codes.Canceled, context.Canceled.Error()).Err(), status.New(codes.Canceled, context.Canceled.Error()).Err()}, "2 errors: rpc error: code = Canceled desc = context canceled; rpc error: code = Canceled desc = context canceled", http.StatusInternalServerError},
		{"mixed context and rpc cancelled", util.MultiError{context.Canceled, status.New(codes.Canceled, context.Canceled.Error()).Err()}, "2 errors: context canceled; rpc error: code = Canceled desc = context canceled", http.StatusInternalServerError},
		{"mixed context, rpc cancelled and another", util.MultiError{errors.New("standard error"), context.Canceled, status.New(codes.Canceled, context.Canceled.Error()).Err()}, "3 errors: standard error; context canceled; rpc error: code = Canceled desc = context canceled", http.StatusInternalServerError},
		{"cancelled storage", promql.ErrStorage{Err: context.Canceled}, ErrClientCanceled, StatusClientClosedRequest},
		{"orgid", user.ErrNoOrgID, user.ErrNoOrgID.Error(), http.StatusBadRequest},
		{"deadline", context.DeadlineExceeded, ErrDeadlineExceeded, http.StatusGatewayTimeout},
		{"deadline multi", util.MultiError{context.DeadlineExceeded, context.DeadlineExceeded}, ErrDeadlineExceeded, http.StatusGatewayTimeout},
		{"rpc deadline", status.New(codes.DeadlineExceeded, context.DeadlineExceeded.Error()).Err(), ErrDeadlineExceeded, http.StatusGatewayTimeout},
		{"rpc deadline multi", util.MultiError{status.New(codes.DeadlineExceeded, context.DeadlineExceeded.Error()).Err(), status.New(codes.DeadlineExceeded, context.DeadlineExceeded.Error()).Err()}, ErrDeadlineExceeded, http.StatusGatewayTimeout},
		{"mixed context and rpc deadline", util.MultiError{context.DeadlineExceeded, status.New(codes.DeadlineExceeded, context.DeadlineExceeded.Error()).Err()}, ErrDeadlineExceeded, http.StatusGatewayTimeout},
		{"mixed context, rpc deadline and another", util.MultiError{errors.New("standard error"), context.DeadlineExceeded, status.New(codes.DeadlineExceeded, context.DeadlineExceeded.Error()).Err()}, "3 errors: standard error; context deadline exceeded; rpc error: code = DeadlineExceeded desc = context deadline exceeded", http.StatusInternalServerError},
		{"parse error", logqlmodel.ParseError{}, "parse error : ", http.StatusBadRequest},
		{"httpgrpc", httpgrpc.Errorf(http.StatusBadRequest, errors.New("foo").Error()), "foo", http.StatusBadRequest},
		{"internal", errors.New("foo"), "foo", http.StatusInternalServerError},
		{"query error", storage_errors.ErrQueryMustContainMetricName, storage_errors.ErrQueryMustContainMetricName.Error(), http.StatusBadRequest},
		{"wrapped query error", fmt.Errorf("wrapped: %w", storage_errors.ErrQueryMustContainMetricName), "wrapped: " + storage_errors.ErrQueryMustContainMetricName.Error(), http.StatusBadRequest},
		{"multi mixed", util.MultiError{context.Canceled, context.DeadlineExceeded}, "2 errors: context canceled; context deadline exceeded", http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(tt.err, rec)
			require.Equal(t, tt.expectedStatus, rec.Result().StatusCode)
			b, err := io.ReadAll(rec.Result().Body)
			if err != nil {
				t.Fatal(err)
			}
			require.Equal(t, tt.msg, string(b[:len(b)-1]))
		})
	}
}
