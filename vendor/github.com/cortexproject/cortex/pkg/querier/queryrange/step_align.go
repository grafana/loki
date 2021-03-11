package queryrange

import (
	"context"
)

// StepAlignMiddleware aligns the start and end of request to the step to
// improved the cacheability of the query results.
var StepAlignMiddleware = MiddlewareFunc(func(next Handler) Handler {
	return stepAlign{
		next: next,
	}
})

type stepAlign struct {
	next Handler
}

func (s stepAlign) Do(ctx context.Context, r Request) (Response, error) {
	start := (r.GetStart() / r.GetStep()) * r.GetStep()
	end := (r.GetEnd() / r.GetStep()) * r.GetStep()
	return s.next.Do(ctx, r.WithStartEnd(start, end))
}
