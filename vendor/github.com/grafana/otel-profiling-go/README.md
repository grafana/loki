# Profiling Instrumentation for OpenTelemetry Go SDK

The package provides means to integrate tracing with profiling. More specifically, a `TracerProvider` implementation
that annotates profiling data with `trace_id` and `span_id` [pprof tags](https://github.com/google/pprof/blob/master/doc/README.md#tag-filtering),
making it possible to filter the profile of a particular trace or span in [Pyroscope](https://grafana.com/docs/pyroscope/latest/).

Note that the module does not control `pprof` profiler itself – it still needs to be started for profiles to be
collected. This can be done either via `runtime/pprof` package, or using the [Pyroscope client](https://github.com/grafana/pyroscope-go).

By default:
- The `trace_id` label is added on the local root span and inherited by every descendant span through pprof's context
  label propagation.
- The `span_id` label and `pyroscope.profile.id` span attribute are set on the local root span only (the first span
  created locally) — this is the join key Grafana's "Traces to profiles" UI uses today. Presence of the attribute
  does not necessarily indicate that the span has a profile: stack trace samples might not be collected if the
  utilized CPU time is less than the sample interval (10ms).

Limitations:
- Only CPU profiling is fully supported at the moment.

## Options

- `WithSpanIDLabelScope(Scope)` — control whether the `span_id` label is emitted: `ScopeNone` (off),
  `ScopeRootSpan` (default — local root only, descendants inherit the root's value), or `ScopeAllSpans`
  (every span emits its own). `ScopeAllSpans` significantly increases label cardinality.
- `WithSpanNameLabelScope(Scope)` — same, for the `span_name` label.

### Trace spans profiles

To start profiling trace spans, you need to include our go module in your app:

```
go get github.com/grafana/otel-profiling-go
```

Then add the pyroscope tracer provider:

```go
package main

import (
	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/grafana/pyroscope-go"
)

func main() {
	// Initialize your tracer provider as usual.
	tp := initTracer()

	// Wrap it with otelpyroscope tracer provider.
	otel.SetTracerProvider(otelpyroscope.NewTracerProvider(tp))

	// If you're using Pyroscope Go SDK, initialize pyroscope profiler.
	_, _ = pyroscope.Start(pyroscope.Config{
		ApplicationName: "my-service",
		ServerAddress:   "http://localhost:4040",
	})

	// Your code goes here.
}
```

Tracing integration is supported in pull mode as well: if you scrape profiles using Grafana Agent, you should
make sure that the pyroscope `service_name` label matches `service.name` attribute specified in the OTel SDK configuration.
Please refer to the [Grafana Agent](https://grafana.com/docs/pyroscope/latest/configure-client/grafana-agent/go_pull/)
documentation to learn more.

## Example

You can find a complete example setup with Grafana Tempo in the [Pyroscope repository](https://github.com/grafana/pyroscope/tree/main/examples/tracing/golang-push).

![image](https://github.com/grafana/otel-profiling-go/assets/12090599/31e33cd1-818b-4116-b952-c9ec7b1fb593)
