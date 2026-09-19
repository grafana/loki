package fakeauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestSetupAuthMiddlewareDisabled(t *testing.T) {
	tenants := []string{"fake", "anonymous", "my-org"}

	for _, tenant := range tenants {
		t.Run(tenant, func(t *testing.T) {
			cfg := &server.Config{}
			var got string

			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				id, err := user.ExtractOrgID(r.Context())
				require.NoError(t, err)
				got = id
			})

			mw := SetupAuthMiddleware(cfg, false, nil, tenant)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			mw.Wrap(next).ServeHTTP(httptest.NewRecorder(), req)

			require.Equal(t, tenant, got)
		})
	}
}

func TestSetupAuthMiddlewareEnabled(t *testing.T) {
	cfg := &server.Config{}

	// When auth is enabled the returned middleware is middleware.AuthenticateUser.
	// We just verify the gRPC interceptor slots were populated correctly.
	mw := SetupAuthMiddleware(cfg, true, []string{"/some.Service/Method"}, "unused")
	require.NotNil(t, mw)
	require.Len(t, cfg.GRPCMiddleware, 1)
	require.Len(t, cfg.GRPCStreamMiddleware, 1)
}

func TestSetupAuthMiddlewareGRPCUnary(t *testing.T) {
	cfg := &server.Config{}
	SetupAuthMiddleware(cfg, false, nil, "my-tenant")
	require.Len(t, cfg.GRPCMiddleware, 1)

	var gotTenant string
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		id, err := user.ExtractOrgID(ctx)
		require.NoError(t, err)
		gotTenant = id
		return nil, nil
	}

	_, err := cfg.GRPCMiddleware[0](context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	require.NoError(t, err)
	require.Equal(t, "my-tenant", gotTenant)
}
