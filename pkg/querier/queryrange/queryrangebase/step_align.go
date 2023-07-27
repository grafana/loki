package queryrangebase

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

func (s stepAlign) Do(ctx context.Context, req Request) (Response, error) {
	start := (req.GetStart() / req.GetStep()) * req.GetStep()
	end := (req.GetEnd() / req.GetStep()) * req.GetStep()
	return s.next.Do(ctx, req.WithStartEnd(start, end))
}
