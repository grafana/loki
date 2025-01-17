package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func Test_onPanic(t *testing.T) {
	rec := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	RecoveryHTTPMiddleware.
		Wrap(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("foo bar")
		})).
		ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	require.Error(t, RecoveryGRPCStreamInterceptor(nil, fakeStream{}, nil, grpc.StreamHandler(func(_ interface{}, _ grpc.ServerStream) error {
		panic("foo")
	})))

	_, err = RecoveryGRPCUnaryInterceptor(context.Background(), nil, nil, grpc.UnaryHandler(func(_ context.Context, _ interface{}) (interface{}, error) {
		panic("foo")
	}))
	require.Error(t, err)

	_, err = RecoveryMiddleware.
		Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (_ queryrangebase.Response, _ error) {
			panic("foo")
		})).
		Do(context.Background(), nil)
	require.ErrorContains(t, err, "foo")
}

type fakeStream struct{}

func (fakeStream) SetHeader(_ metadata.MD) error  { return nil }
func (fakeStream) SendHeader(_ metadata.MD) error { return nil }
func (fakeStream) SetTrailer(_ metadata.MD)       {}
func (fakeStream) Context() context.Context       { return context.Background() }
func (fakeStream) SendMsg(_ interface{}) error    { return nil }
func (fakeStream) RecvMsg(_ interface{}) error    { return nil }
