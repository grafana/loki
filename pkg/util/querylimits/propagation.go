package querylimits

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/prometheus/common/model"
)

// Context key type used to avoid collisions
type key int

const (
	queryLimitsContextKey key = 1

	HTTPHeaderQueryLimitsKey = "X-Loki-Query-Limits"
)

// NOTE: we use custom `model.Duration` instead of standard `time.Duration` because,
// to support user-friendly duration format (e.g: "1h30m45s") in JSON value.
type QueryLimits struct {
	MaxQueryLength   model.Duration `json:"max_query_length"`
	MaxQueryLookback model.Duration `json:"max_query_lookback"`
	// this should be changed to max bytes
	MaxEntriesLimitPerQuery int            `json:"max_entries_limit_per_query"`
	QueryTimeout            model.Duration `json:"query_timeout"`
	RequiredLabels          []string       `json:"required_labels"`
}

// These are the defaults for the QueryLimitsStruct.
// When we see these values in the middleware we should use
// the tenants limit for this value instead of the default from
// the query limit struct.
//var defaultQueryLimits = QueryLimits{
//	// should we allow 0 durations and entries?
//	MaxQueryLength:          -1,
//	MaxQueryLookback:        -1,
//	QueryTimeout:            -1,
//	MaxEntriesLimitPerQuery: -1,
//}

func UnmarshalQueryLimits(data []byte) (*QueryLimits, error) {
	limits := &QueryLimits{}
	err := json.Unmarshal(data, limits)
	return limits, err
}

func MarshalQueryLimits(limits *QueryLimits) ([]byte, error) {
	return json.Marshal(limits)
}

// InjectQueryLimitsHTTP adds the query limits to the request headers.
func InjectQueryLimitsHTTP(r *http.Request, limits *QueryLimits) error {
	// Ensure any existing policy sets are erased
	r.Header.Del(HTTPHeaderQueryLimitsKey)

	encodedLimits, err := MarshalQueryLimits(limits)
	if err != nil {
		return err
	}
	r.Header.Add(HTTPHeaderQueryLimitsKey, string(encodedLimits))
	return nil
}

// ExtractQueryLimitsHTTP retrieves the query limit policy from the HTTP header and returns it.
func ExtractQueryLimitsHTTP(r *http.Request) (*QueryLimits, error) {
	headerValues := r.Header.Values(HTTPHeaderQueryLimitsKey)

	// Iterate through each set header value
	for _, headerValue := range headerValues {
		return UnmarshalQueryLimits([]byte(headerValue))

	}

	return nil, nil
}

// ExtractQueryLimitsContext gets the embedded limits from the context
func ExtractQueryLimitsContext(ctx context.Context) *QueryLimits {
	source, ok := ctx.Value(queryLimitsContextKey).(*QueryLimits)

	if !ok {
		return nil
	}

	return source
}

// InjectQueryLimitsContext returns a derived context containing the provided query limits
func InjectQueryLimitsContext(ctx context.Context, limits QueryLimits) context.Context {
	return context.WithValue(ctx, interface{}(queryLimitsContextKey), &limits)
}
