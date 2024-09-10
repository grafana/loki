kotel
===

Kotel is an OpenTelemetry instrumentation plug-in package for franz-go. It
provides [tracing](https://pkg.go.dev/go.opentelemetry.io/otel/trace)
and [metrics](https://pkg.go.dev/go.opentelemetry.io/otel/metric) options
through
a [`kgo.Hook`](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Hook). With
kotel, you can trace records produced or consumed with franz-go. You can pass
parent traces into records and extract parent traces from records. It also
tracks metrics related to connections, errors, and bytes transferred.

To learn more about how to use kotel, see the usage sections in the README and
refer to the [OpenTelemetry documentation](https://opentelemetry.io/docs) for
additional information about OpenTelemetry and how it can be used in your
franz-go projects.

## Tracing

kotel provides tracing capabilities for Kafka using OpenTelemetry
specifications. It allows for the creation of three different span
operations: "publish", "receive", and "process". Additionally, it also provides
a set of attributes to use with these spans.

### How it works

The kotel tracer module uses hooks to automatically create and close "publish"
and "receive" spans as a `kgo.Record` flows through the application. However,
for the "process" span, it uses a convenience method that must be manually
invoked and closed in the consumer code to capture processing.

The following table provides a visual representation of the lineage of the
span operations:

| Order | Hook/Method                     | Operation | State |
|-------|---------------------------------|-----------|-------|
| 1     | kgo.HookProduceRecordBuffered   | Publish   | Start |
| 2     | kgo.HookProduceRecordUnbuffered | Publish   | End   |
| 3     | kgo.HookFetchRecordBuffered     | Receive   | Start |
| 4     | kgo.HookFetchRecordUnbuffered   | Receive   | End   |
| 5     | kotel.Tracer.WithProcessSpan    | Process   | Start |

### Getting started

To start using kotel for tracing, you will need to:

1. Set up a tracer provider
2. Configure any desired tracer options
3. Create a new kotel tracer
4. Create a new kotel service hook
5. Create a new Kafka client and pass in the kotel hook

Here's an example of how you might do this:

```go
// Initialize tracer provider.
tracerProvider, err := initTracerProvider()

// Create a new kotel tracer.
tracerOpts := []kotel.TracerOpt{
	kotel.TracerProvider(tracerProvider),
	kotel.TracerPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})),
}
tracer := kotel.NewTracer(tracerOpts...)

// Create a new kotel service.
kotelOps := []kotel.Opt{
	kotel.WithTracer(tracer),
}
kotelService := kotel.NewKotel(kotelOps...)

// Create a new Kafka client.
cl, err := kgo.NewClient(
	// Pass in the kotel hook.
	kgo.WithHooks(kotelService.Hooks()...),
	// ...other opts.
)
```

### Sending records

When producing a record with franz-go, it will traced by kotel. To include
parent traces, pass in an instrumented context.

Here's an example of how to do this:

```go
func httpHandler(w http.ResponseWriter, r *http.Request) {
	// Start a new span with options.
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes([]attribute.KeyValue{attribute.String("some-key", "foo")}...),
	}
	ctx, span := tracer.Start(r.Context(), "request", opts...)
	// End the span when function exits.
	defer span.End()

	var wg sync.WaitGroup
	wg.Add(1)
	record := &kgo.Record{Topic: "topic", Value: []byte("foo")}
	// Pass in the context from the tracer.Start() call to ensure that the span
	// created is linked to the parent span.
	cl.Produce(ctx, record, func(_ *kgo.Record, err error) {
		defer wg.Done()
		if err != nil {
			fmt.Printf("record had a produce error: %v\n", err)
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
	})
	wg.Wait()
}
```

### Processing Records

Use the `kotel.Tracer.WithProcessSpan` method to start a "process" span. Make
sure to end the span after you finish processing the record. The trace can be
continued to the next processing step if desired.

Here is an example of how you might do this:

```go
func processRecord(record *kgo.Record, tracer *kotel.Tracer) {
	ctx, span := tracer.WithProcessSpan(record)
	// Process the record here.
	// End the span when function exits.
	defer span.End()
	// optionally pass the context to the next processing step.
	fmt.Printf(
		"processed offset '%s' with key '%s' and value '%s'\n",
		strconv.FormatInt(record.Offset, 10),
		string(record.Key),
		string(record.Value),
	)
}
```

## Metrics

The kotel meter module tracks various metrics related to the processing of
records, such as the number of successful and unsuccessful connections, bytes
written and read, and the number of buffered records. These metrics are all
counters and are tracked under the following names:

```
messaging.kafka.connects.count{node_id = "#{node}"}
messaging.kafka.connect_errors.count{node_id = "#{node}"}
messaging.kafka.disconnects.count{node_id = "#{node}"}
messaging.kafka.write_errors.count{node_id = "#{node}"}
messaging.kafka.write_bytes{node_id = "#{node}"}
messaging.kafka.read_errors.count{node_id = "#{node}"}
messaging.kafka.read_bytes.count{node_id = "#{node}"}
messaging.kafka.produce_bytes.count{node_id = "#{node}", topic = "#{topic}"}
messaging.kafka.produce_records.count{node_id = "#{node}", topic = "#{topic}"}
messaging.kafka.fetch_bytes.count{node_id = "#{node}", topic = "#{topic}"}
messaging.kafka.fetch_records.count{node_id = "#{node}", topic = "#{topic}"}
```

### Getting started

To start using kotel for metrics, you will need to:

1. Set up a meter provider
2. Configure any desired meter options
3. Create a new kotel meter
4. Create a new kotel service hook
5. Create a new Kafka client and pass in the kotel hook

Here's an example of how you might do this:

```go
// Initialize meter provider.
meterProvider, err := initMeterProvider()

// Create a new kotel meter.
meterOpts := []kotel.MeterOpt{kotel.MeterProvider(meterProvider)}
meter := kotel.NewMeter(meterOpts...)

// Pass the meter to NewKotel hook.
kotelOps := []kotel.Opt{
	kotel.WithMeter(meter),
}

// Create a new kotel service.
kotelService := kotel.NewKotel(kotelOps...)

// Create a new Kafka client.
cl, err := kgo.NewClient(
	// Pass in the kotel hook.
	kgo.WithHooks(kotelService.Hooks()...),
	// ...other opts.
)
```
