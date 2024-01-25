# Span Profiler for OpenTracing-Go

## Overview

The Span Profiler for OpenTracing-Go is a package that seamlessly integrates `opentracing-go` instrumentation with
profiling through the use of pprof labels.

Accessing trace span profiles is made convenient through the Grafana Explore view. You can find a complete example setup
with Grafana Tempo in the [Pyroscope repository](https://github.com/grafana/pyroscope/tree/main/examples/tracing/tempo):

![image](https://github.com/grafana/otel-profiling-go/assets/12090599/31e33cd1-818b-4116-b952-c9ec7b1fb593)

## Usage

There are two primary ways to use the Span Profiler:

### 1. Wrap the Global Tracer.

You can wrap the global tracer using `spanprofiler.NewTracer`:

```go
import (
    "github.com/opentracing/opentracing-go"
    "github.com/grafana/dskit/spanprofiler"
)

func main() {
    // Initialize your OpenTracing tracer
    tracer := opentracing.GlobalTracer()
    // Wrap it with the tracer-profiler 
    wrappedTracer := spanprofiler.NewTracer(tracer)
    // Use the wrapped tracer in your application
    opentracing.SetGlobalTracer(wrappedTracer)

    // Or, as an oneliner:
    // opentracing.SetGlobalTracer(spanprofiler.NewTracer(opentracing.GlobalTracer()))

    // Your application logic here
}
```

For efficiency, the tracer selectively records profiles for _root_ spans — the initial _local_ span in a process — since
a trace may encompass thousands of spans. All stack trace samples accumulated during the execution of their child spans
contribute to the root span's profile. In practical terms, this signifies that, for instance, an HTTP request results
in a singular profile, irrespective of the numerous spans within the trace. It's important to note that these profiles
don't extend beyond the boundaries of a single process.

The limitation of this approach is that only spans created within the same goroutine, or its children, as the parent are
taken into account. Consequently, in scenarios involving asynchronous execution, where the parent span context is passed
to another goroutine, explicit profiling becomes necessary using `spanprofiler.StartSpanFromContext`.

### 2. Profile individual spans.

The `spanprofiler.StartSpanFromContext` function allows you to granularly control which spans to profile:

```go
func YourOperationName(ctx context.Background()) {
    // Start a span and enable profiling for it
    span, ctx := spanprofiler.StartSpanFromContext(ctx, "YourOperationName", tracer)
    defer span.Finish() // Finish the span when done

    // Use the span in your application logic
}
```

The function guarantees that the span is to be profiled.

Both methods can be employed either in conjunction or independently. Our recommendation is to utilize the tracer for
seamless integration, reserving explicit span profiling only for cases where spans are spawned in detached goroutines.

## Implementation details

When a new trace span is created, and is eligible for profiling, the tracer sets `span_id` and `span_name` [pprof labels](https://github.com/google/pprof/blob/master/doc/README.md#tag-filtering)
that point to the respective span. These labels are stored in the goroutine's local storage and inherited by any
subsequent child goroutines.

`span_name` is available as a regular label and can be used in the query expressions. For example, the following query 
will show you profile for the code that is not covered with traces:
```
{service_name="my-service",span_name=""}
```

Additionally, trace spans are identified by the `pyroscope.profile.id` attribute, indicating the associated profile.
This allows to find such spans in the trace view (in the screenshot) and fetch profiles for specific spans.

It's important to note that the presence of this attribute does not guarantee profile availability; stack trace samples
might not be collected if the CPU time utilized falls below the sample interval (10ms).

It is crucial to understand that this module doesn't directly control the pprof profiler; its initialization is still
necessary for profile collection. This initialization can be achieved through the `runtime/pprof` package, or using the
[Pyroscope client](https://github.com/grafana/pyroscope-go).

Limitations:
 - Only CPU profiling is fully supported at the moment.
 - Only [Jaeger tracer](https://github.com/jaegertracing/jaeger-client-go) implementation is supported.

## Performance implications

The typical performance impact is generally imperceptible and primarily arises from the cost of pprof labeling. However,
intensive use of pprof labels may have negative impact on the profiled application.

In the case of the tracer provided by this package, the `StartSpan` method wrapper introduces an approximate 20% increase
in CPU time compared to the original call. In vase majority of cases, the overhead constitutes less than 0.01% of the total
CPU time and is considered safe for deployment in production systems.
