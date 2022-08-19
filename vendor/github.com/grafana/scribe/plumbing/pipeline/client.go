package pipeline

import "context"

type Client interface {
	// Validate is ran internally before calling Run or Parallel and allows the client to effectively configure per-step requirements
	// For example, Drone steps MUST have an image so the Drone client returns an error in this function when the provided step does not have an image.
	// If the error encountered is not critical but should still be logged, then return a plumbing.ErrorSkipValidation.
	// The error is checked with `errors.Is` so the error can be wrapped with fmt.Errorf.
	Validate(Step) error

	// Done must be ran at the end of the pipeline.
	// This is typically what takes the defined pipeline steps, runs them in the order defined, and produces some kind of output.
	Done(context.Context, Walker) error
}
