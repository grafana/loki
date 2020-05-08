package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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

	require.Error(t, RecoveryGRPCStreamInterceptor(nil, fakeStream{}, nil, grpc.StreamHandler(func(srv interface{}, stream grpc.ServerStream) error {
		panic("foo")
	})))

	_, err = RecoveryGRPCUnaryInterceptor(context.Background(), nil, nil, grpc.UnaryHandler(func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("foo")
	}))
	require.Error(t, err)
}

type fakeStream struct{}

func (fakeStream) SetHeader(_ metadata.MD) error  { return nil }
func (fakeStream) SendHeader(_ metadata.MD) error { return nil }
func (fakeStream) SetTrailer(_ metadata.MD)       {}
func (fakeStream) Context() context.Context       { return context.Background() }
func (fakeStream) SendMsg(m interface{}) error    { return nil }
func (fakeStream) RecvMsg(m interface{}) error    { return nil }
