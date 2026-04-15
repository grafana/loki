// Package fakeauth provides middlewares thats injects a fake userID, so the rest of the code
// can continue to be multitenant.
package fakeauth

import (
	"context"
	"net/http"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/user"
	"google.golang.org/grpc"
)

// SetupAuthMiddleware for the given server config.
func SetupAuthMiddleware(config *server.Config, enabled bool, noGRPCAuthOn []string) middleware.Interface {
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
		fakeGRPCAuthUniaryMiddleware,
	)
	config.GRPCStreamMiddleware = append(config.GRPCStreamMiddleware,
		fakeGRPCAuthStreamMiddleware,
	)
	return fakeHTTPAuthMiddleware
}

var fakeHTTPAuthMiddleware = middleware.Func(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := user.InjectOrgID(r.Context(), "fake")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
})

var fakeGRPCAuthUniaryMiddleware = func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx = user.InjectOrgID(ctx, "fake")
	return handler(ctx, req)
}

var fakeGRPCAuthStreamMiddleware = func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := user.InjectOrgID(ss.Context(), "fake")
	return handler(srv, serverStream{
		ctx:          ctx,
		ServerStream: ss,
	})
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss serverStream) Context() context.Context {
	return ss.ctx
}
