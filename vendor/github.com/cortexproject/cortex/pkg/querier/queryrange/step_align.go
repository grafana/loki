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

func (s stepAlign) Do(ctx context.Context, r *Request) (*APIResponse, error) {
	r.Start = (r.Start / r.Step) * r.Step
	r.End = (r.End / r.Step) * r.Step
	return s.next.Do(ctx, r)
}
