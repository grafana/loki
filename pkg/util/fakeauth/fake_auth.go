// Package fakeauth provides middlewares that inject a tenant ID so the rest of
// the code can continue to be multi-tenant even when auth is disabled.
package fakeauth

import (
	"context"
	"net/http"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/user"
	"google.golang.org/grpc"
)

// SetupAuthMiddleware configures auth middleware for the given server config.
// When enabled is false, noAuthTenant is injected as the org ID into every
// request so downstream code can assume a tenant is always present.
func SetupAuthMiddleware(config *server.Config, enabled bool, noGRPCAuthOn []string, noAuthTenant string) middleware.Interface {
	if enabled {
		ignoredMethods := map[string]bool{}
		for _, m := range noGRPCAuthOn {
			ignoredMethods[m] = true
		}

		config.GRPCMiddleware = append(config.GRPCMiddleware, func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
			if ignoredMethods[info.FullMethod] {
				return handler(ctx, req)
			}
			return middleware.ServerUserHeaderInterceptor(ctx, req, info, handler)
		})

		config.GRPCStreamMiddleware = append(config.GRPCStreamMiddleware,
			func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				if ignoredMethods[info.FullMethod] {
					return handler(srv, ss)
				}
				return middleware.StreamServerUserHeaderInterceptor(srv, ss, info, handler)
			},
		)

		return middleware.AuthenticateUser
	}

	config.GRPCMiddleware = append(config.GRPCMiddleware,
		func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			ctx = user.InjectOrgID(ctx, noAuthTenant)
			return handler(ctx, req)
		},
	)
	config.GRPCStreamMiddleware = append(config.GRPCStreamMiddleware,
		func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			ctx := user.InjectOrgID(ss.Context(), noAuthTenant)
			return handler(srv, serverStream{
				ctx:          ctx,
				ServerStream: ss,
			})
		},
	)
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := user.InjectOrgID(r.Context(), noAuthTenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss serverStream) Context() context.Context {
	return ss.ctx
}
