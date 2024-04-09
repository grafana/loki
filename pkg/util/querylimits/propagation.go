package querylimits

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util/flagext"
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
	MaxQueryLength          model.Duration   `json:"maxQueryLength,omitempty"`
	MaxQueryRange           model.Duration   `json:"maxQueryInterval,omitempty"`
	MaxQueryLookback        model.Duration   `json:"maxQueryLookback,omitempty"`
	MaxEntriesLimitPerQuery int              `json:"maxEntriesLimitPerQuery,omitempty"`
	QueryTimeout            model.Duration   `json:"maxQueryTime,omitempty"`
	RequiredLabels          []string         `json:"requiredLabels,omitempty"`
	RequiredNumberLabels    int              `json:"minimumLabelsNumber,omitempty"`
	MaxQueryBytesRead       flagext.ByteSize `json:"maxQueryBytesRead,omitempty"`
}

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
	return InjectQueryLimitsHeader(&r.Header, limits)
}

// InjectQueryLimitsHeader adds the query limits to the headers.
func InjectQueryLimitsHeader(h *http.Header, limits *QueryLimits) error {
	// Ensure any existing policy sets are erased
	h.Del(HTTPHeaderQueryLimitsKey)

	encodedLimits, err := MarshalQueryLimits(limits)
	if err != nil {
		return err
	}
	h.Add(HTTPHeaderQueryLimitsKey, string(encodedLimits))
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
