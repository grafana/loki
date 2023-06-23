package querylimits

import (
	"net/http"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

type tripperwareWrapper struct {
	next    http.RoundTripper
	wrapped http.RoundTripper
}

// WrapTripperware wraps the existing tripperware to make sure the query limit policy headers are propagated
func WrapTripperware(existing queryrangebase.Tripperware) queryrangebase.Tripperware {
	return func(next http.RoundTripper) http.RoundTripper {
		limitsTrw := &tripperwareWrapper{
			next: next,
		}
		limitsTrw.wrapped = existing(queryrangebase.RoundTripFunc(limitsTrw.PostWrappedRoundTrip))
		return limitsTrw
	}
}

func (t *tripperwareWrapper) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()

	limits := ExtractQueryLimitsContext(ctx)

	if limits != nil {
		ctx = InjectQueryLimitsContext(ctx, *limits)
		r = r.Clone(ctx)
	}

	return t.wrapped.RoundTrip(r)
}

func (t *tripperwareWrapper) PostWrappedRoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()

	limits := ExtractQueryLimitsContext(ctx)

	if limits != nil {
		err := InjectQueryLimitsHTTP(r, limits)
		if err != nil {
			return nil, err
		}
	}

	return t.next.RoundTrip(r)
}
