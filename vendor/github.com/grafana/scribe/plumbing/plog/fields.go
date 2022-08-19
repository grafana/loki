package plog

import (
	"context"

	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/stringutil"
	"github.com/opentracing/opentracing-go"
	"github.com/sirupsen/logrus"
	"github.com/uber/jaeger-client-go"
)

// TracingFields adds fields that are derived from the context.Context.
// Tracing has handled using context.Context, and if tracing is enabled, then everything happening should be within a tracing span / trace
func TracingFields(ctx context.Context) logrus.Fields {
	fields := logrus.Fields{}

	span := opentracing.SpanFromContext(ctx)
	if span != nil {
		if jaegerCtx, ok := span.Context().(jaeger.SpanContext); ok {
			fields["trace_id"] = jaegerCtx.TraceID().String()
			fields["span_id"] = jaegerCtx.SpanID().String()
		}
	}

	return fields
}

func StepFields(step pipeline.Step) logrus.Fields {
	return logrus.Fields{
		"step":   step.Name,
		"serial": step.ID,
	}
}

func PipelineFields(opts pipeline.CommonOpts) logrus.Fields {
	return logrus.Fields{
		"build_id": opts.Args.BuildID,
		"pipeline": stringutil.Slugify(opts.Name),
	}
}

func Combine(field ...logrus.Fields) logrus.Fields {
	fields := logrus.Fields{}

	for _, m := range field {
		for k, v := range m {
			fields[k] = v
		}
	}

	return fields
}

func DefaultFields(ctx context.Context, step pipeline.Step, opts pipeline.CommonOpts) logrus.Fields {
	return Combine(TracingFields(ctx), StepFields(step), PipelineFields(opts))
}
