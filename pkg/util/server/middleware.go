package server

import (
	"context"
	"net/http"

	"github.com/grafana/loki/pkg/entitlement"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// NewPrepopulateMiddleware creates a middleware which will parse incoming http forms.
// This is important because some endpoints can POST x-www-form-urlencoded bodies instead of GET w/ query strings.
func NewPrepopulateMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			err := req.ParseForm()
			if err != nil {
				WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
				return

			}
			next.ServeHTTP(w, req)
		})
	})
}

func ResponseJSONMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			next.ServeHTTP(w, req)
		})
	})
}

// AuthenticateUserMultiTenancy propagates the org and user ID from HTTP headers back to the request's context.
// Copied and modified from weaveworks/common/middleware/http_auth.go::AuthenticateUser to add clientUserID
var AuthenticateUserMultiTenancy = middleware.Func(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ctx, err := user.ExtractOrgIDFromHTTPRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		entitlement.InjectClientUserID(&ctx, r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
})

// AuthenticateUserSingleTenancy propagates the org and user ID from HTTP headers back to the request's context.
// Copied and modified from weaveworks/common/middleware/http_auth.go::AuthenticateUser to add clientUserID
var AuthenticateUserSingleTenancy = middleware.Func(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := user.InjectOrgID(r.Context(), "fake")
		entitlement.InjectClientUserID(&ctx, r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
})

// ClientUserHeaderInterceptor propagates the user ID from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
// Copied and modified from weaveworks/common/middleware/grpc_auth.go to inject ClientUserID
func ClientUserHeaderInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, err := injectClientUserIDIntoGRPCRequest(ctx)
	if err != nil {
		return err
	}

	return invoker(ctx, method, req, reply, cc, opts...)
}

// StreamClientUserHeaderInterceptor propagates the user ID from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
// For streaming gRPC requests.
// Copied and modified from weaveworks/common/middleware/grpc_auth.go to inject ClientUserID
func StreamClientUserHeaderInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, err := injectClientUserIDIntoGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return streamer(ctx, desc, cc, method, opts...)
}

// ServerClientUserHeaderInterceptor propagates the user ID from the gRPC metadata back to our context.
// Copied and modified from weaveworks/common/middleware/grpc_auth.go to extract ClientUserID
func ServerClientUserHeaderInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	_, ctx, err := extractClientUserIDFromGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

// StreamServerClientUserHeaderInterceptor propagates the user ID from the gRPC metadata back to our context.
// Copied and modified from weaveworks/common/middleware/grpc_auth.go to extract ClientUserID
func StreamServerClientUserHeaderInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	_, ctx, err := extractClientUserIDFromGRPCRequest(ss.Context())
	if err != nil {
		return err
	}

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

const (
	// OrgIDHeaderName  = "X-Scope-OrgID"
	// UserIDHeaderName = "X-Scope-UserID"

	lowerOrgIDHeaderName  = "x-scope-orgid"
	lowerUserIDHeaderName = "x-scope-userid"
)

func extractClientUserIDFromGRPCRequest(ctx context.Context) (string, context.Context, error) {
	// extract userid from grpc metadata into ctx
	// only when cname is trusted
	if p, ok := peer.FromContext(ctx); ok {
		if mtls, ok := p.AuthInfo.(credentials.TLSInfo); ok {
			// client cname is always at 0th position
			cname := mtls.State.PeerCertificates[0].Subject.CommonName
			if entitlement.CnameIsTrusted(cname) {
				md, ok := metadata.FromIncomingContext(ctx)
				if !ok {
					return "", ctx, user.ErrNoUserID
				}

				userIDs, okUserID := md[lowerUserIDHeaderName]

				if !okUserID || len(userIDs) != 1 {
					return "", ctx, user.ErrNoUserID
				}

				return userIDs[0], user.InjectUserID(ctx, userIDs[0]), nil
			}
		}
	}

	return "fake", ctx, nil
}

func injectClientUserIDIntoGRPCRequest(ctx context.Context) (context.Context, error) {
	// everyone can inject userid into gRPC metadata because it's validated during extract
	// don't need to check client's cname
	userID, err := user.ExtractUserID(ctx)
	if err != nil {
		// if ctx doesn't have userid, outgoing gRPC will use "fake" by default (e.g. healthcheck)
		userID = "fake"
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	newCtx := ctx
	if userIDs, ok := md[lowerUserIDHeaderName]; ok {
		if len(userIDs) == 1 {
			if userIDs[0] != userID {
				return ctx, user.ErrDifferentUserIDPresent
			}
		} else {
			return ctx, user.ErrTooManyUserIDs
		}
	} else {
		md = md.Copy()
		md[lowerUserIDHeaderName] = []string{userID}
		newCtx = metadata.NewOutgoingContext(ctx, md)
	}

	return newCtx, nil
}
