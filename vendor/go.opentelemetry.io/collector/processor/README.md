# General Information

Processors are used at various stages of a pipeline. Generally, a processor
pre-processes data before it is exported (e.g. modify attributes or sample).

Some important aspects of pipelines and processors to be aware of:
- [Recommended Processors](#recommended-processors)
- [Data Ownership](#data-ownership)
- [Exclusive Ownership](#exclusive-ownership)
- [Shared Ownership](#shared-ownership)
- [Ordering Processors](#ordering-processors)
- [Creating Custom Processor](#creating-custom-processors)

Supported processors (sorted alphabetically):
- [Batch Processor](batchprocessor/README.md)
- [Memory Limiter Processor](memorylimiterprocessor/README.md)

The [contrib repository](https://github.com/open-telemetry/opentelemetry-collector-contrib)
 has more processors that can be added to a custom build of the Collector.

## Recommended Processors

By default, no processors are enabled. Depending on the data source, it may be
recommended that multiple processors be enabled. Processors must be enabled
for every data source and not all processors support all data sources.
In addition, it is important to note that the order of processors matters. The
order in each section below is the best practice. Refer to the individual
processor documentation for more information.

1. [memory_limiter](memorylimiterprocessor/README.md)
2. Any sampling or initial filtering processors
3. Any processor relying on sending source from `Context` (e.g. `k8sattributes`)
3. [batch](batchprocessor/README.md), although prefer using the exporter's batching capabilities
4. Any other processors

## Data Ownership

The ownership of the `pdata.Traces`, `pdata.Metrics` and `pdata.Logs` data in a pipeline
is passed as the data travels through the pipeline. The data is created by the receiver
and then the ownership is passed to the first processor when `ConsumeTraces`/`ConsumeMetrics`/`ConsumeLogs`
function is called.

Note: the receiver may be attached to multiple pipelines, in which case the same data
will be passed to all attached pipelines via a data fan-out connector.

From data ownership perspective pipelines can work in 2 modes:
* Exclusive data ownership
* Shared data ownership

The mode is defined during startup based on data modification intent reported by the
processors. The intent is reported by each processor via `MutatesData` field of
the struct returned by `Capabilities` function. If any processor in the pipeline
declares an intent to modify the data then that pipeline will work in exclusive ownership
mode. In addition, any other pipeline that receives data from a receiver that is attached
to a pipeline with exclusive ownership mode will be also operating in exclusive ownership
mode.

### Exclusive Ownership

In exclusive ownership mode the data is owned exclusively by a particular processor at a
given moment of time, and the processor is free to modify the data it owns.

Exclusive ownership mode is only applicable for pipelines that receive data from the
same receiver. If a pipeline is marked to be in exclusive ownership mode then any data
received from a shared receiver will be cloned at the fan-out connector before passing
further to each pipeline. This ensures that each pipeline has its own exclusive copy of
data, and the data can be safely modified in the pipeline.

The exclusive ownership of data allows processors to freely modify the data while
they own it (e.g. see `attributesprocessor`). The duration of ownership of the data
by processor is from the beginning of `ConsumeTraces`/`ConsumeMetrics`/`ConsumeLogs` 
call until the processor calls the next processor's `ConsumeTraces`/`ConsumeMetrics`/`ConsumeLogs`
function, which passes the ownership to the next processor. After that the processor
must no longer read or write the data since it may be concurrently modified by the
new owner.

Exclusive Ownership mode allows to easily implement processors that need to modify
the data by simply declaring such intent.

### Shared Ownership

In shared ownership mode no particular processor owns the data and no processor is
allowed the modify the shared data.

In this mode no cloning is performed at the fan-out connector of receivers that
are attached to multiple pipelines. In this case all such pipelines will see
the same single shared copy of the data. Processors in pipelines operating in shared
ownership mode are prohibited from modifying the original data that they receive
via `ConsumeTraces`/`ConsumeMetrics`/`ConsumeLogs` call. Processors may only read
the data but must not modify the data.

If the processor needs to modify the data while performing the processing but
does not want to incur the cost of data cloning that Exclusive mode brings then
the processor can declare that it does not modify the data and use any
different technique that ensures original data is not modified. For example,
the processor can implement copy-on-write approach for individual sub-parts of
`pdata.Traces`/`pdata.Metrics`/`pdata.Logs` argument. Any approach that does not
mutate the original `pdata.Traces`/`pdata.Metrics`/`pdata.Logs` is allowed.

If the processor uses such technique it should declare that it does not intend
to modify the original data by setting `MutatesData=false` in its capabilities
to avoid marking the pipeline for Exclusive ownership and to avoid the cost of
data cloning described in Exclusive Ownership section.

## Ordering Processors

The order processors are specified in a pipeline is important as this is the
order in which each processor is applied.

## Creating Custom Processors

To create a custom processor for the OpenTelemetry Collector, you need to implement the processor interface, define the processor's configuration, and register it with the Collector. The process typically involves creating a factory, implementing the required processing logic, and handling configuration options. For a practical example and guidance, refer to the [`processorhelper`](https://pkg.go.dev/go.opentelemetry.io/collector/processor/processorhelper) package, which provides utilities and patterns to simplify processor development.

