package multitenancy

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
)

var errUserIDNotPopulated = errors.New("context didnt posess value for either orgID or label and undefined")

// Pretty similar to the fake_auth middleware, we basically just need a way to inform the handler that
// we intend on using the a label from the data as the orgID rather than from the actual header, and pass
// the label we want to use via context

// InjectLabelForID injects a field into the context that specifies using a label rather than orgID from header
func InjectLabelForID(ctx context.Context, label string, undefined string) context.Context {
	if label == "" || undefined == "" {
		return nil
	}
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

// HTTPAuthMiddlewarePush middleware for injecting value specificying using labels into the push api
func HTTPAuthMiddlewarePush(label string, undefined string) middleware.Func {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := InjectLabelForID(r.Context(), label, undefined)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
}
