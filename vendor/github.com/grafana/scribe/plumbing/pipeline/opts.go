package pipeline

import (
	"io"

	"github.com/grafana/scribe/plumbing"
	"github.com/opentracing/opentracing-go"
	"github.com/sirupsen/logrus"
)

// CommonOpts are provided in the Client's Init function, which includes options that are common to all clients, like
// logging, output, and debug options
type CommonOpts struct {
	Name    string
	Version string
	Output  io.Writer
	Args    *plumbing.PipelineArgs
	Log     *logrus.Logger
	Tracer  opentracing.Tracer
	State   *State
}
