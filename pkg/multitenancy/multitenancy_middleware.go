package multitenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
)

var errUserIDNotPopulated = errors.New("context didnt posess value for either orgID or label and undefined")

// Pretty similar to the fake_auth middleware, we basically just need a way to inform the handler that
// we intend on using the a label from the data as the orgID rather than from the actual header, and pass
// the label we want to use via context

// InjectLabelForID injects a field into the context that specifies using a label rather than orgID from header
func InjectLabelForID(ctx context.Context, label string, undefined string) context.Context {
	newCtx := context.WithValue(ctx, interface{}("useLabelAsOrgID"), label)
	return context.WithValue(newCtx, interface{}("useLabelAsOrgIDUndefined"), undefined)
}

// GetLabelFromContext returns the value set for label and undefined
func GetLabelFromContext(ctx context.Context) (string, string) {
	label, ok1 := ctx.Value("useLabelAsOrgID").(string)
	undefined, ok2 := ctx.Value("useLabelAsOrgIDUndefined").(string)
	if ok1 && ok2 {
		return label, undefined
	}
	return "", ""
}

// GetUserIDFromContextAndStringLabels returns either just userID aka orgID, or label value or finally undefined if neither exist
// designed as a drop in upgrade to the original user.ExtractOrgID
func GetUserIDFromContextAndStringLabels(ctx context.Context, labels string) (string, error) {
	userID, err := user.ExtractOrgID(ctx)
	label, undefined := GetLabelFromContext(ctx)
	if err != nil && (label == "" || undefined == "") {
		return "", err
	} else if err == nil && userID == "" && (label == "" || undefined == "") {
		return "", errUserIDNotPopulated
	}
	// Try get the associated label from the stream, otherwise we can use undefined
	if label != "" {
		re := regexp.MustCompile(label + "=\"([^ ]+)\"")
		values := re.FindStringSubmatch(labels)
		fmt.Println(values)
		if len(values) == 0 {
			return undefined, nil
		} else if len(values) >= 1 {
			return values[1], nil
		}
	} else {
		return userID, nil
	}
	return "", errUserIDNotPopulated
}

// GetUserIDFromContextAndLabels returns userID from context and prometheus labels.Label array
func GetUserIDFromContextAndLabels(ctx context.Context, labels []labels.Label) (string, error) {
	userID, err := user.ExtractOrgID(ctx)
	label, undefined := GetLabelFromContext(ctx)
	if err != nil && (label == "" || undefined == "") {
		return "", err
	} else if err == nil && userID == "" && (label == "" || undefined == "") {
		return "", errUserIDNotPopulated
	}
	// Try get the associated label from the stream, otherwise we can use undefined
	if label != "" {
		value := ""
		for _, streamLabel := range labels {
			if streamLabel.Name == label {
				value = streamLabel.Value
			}
		}
		fmt.Println(value)
		if value == "" {
			return value, nil
		}
		return value, nil
	}
	return userID, nil
}

// HTTPAuthMiddleware middleware use for injecting value specificying using labels
func HTTPAuthMiddleware(label string, undefined string) middleware.Func {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ctx := user.InjectOrgID(r.Context(), nil)
			ctx := InjectLabelForID(r.Context(), label, undefined)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
}

// GRPCAuthUnaryMiddleware auth middleware for GRPC
func GRPCAuthUnaryMiddleware(label string, undefined string) func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		newCtx := InjectLabelForID(ctx, label, undefined)
		return handler(newCtx, req)
	}
}

// GRPCAuthStreamMiddleware auth middleware for GRPC stream
func GRPCAuthStreamMiddleware(label string, undefined string) func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := InjectLabelForID(ss.Context(), label, undefined)
		return handler(srv, serverStream{
			ctx:          ctx,
			ServerStream: ss,
		})
	}
}

// Taken from fake_auth
type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

// Taken from fake_auth
func (ss serverStream) Context() context.Context {
	return ss.ctx
}
