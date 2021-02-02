package telegraf

// Processor is a processor plugin interface for defining new inline processors.
// these are extremely efficient and should be used over StreamingProcessor if
// you do not need asynchronous metric writes.
type Processor interface {
	PluginDescriber

	// Apply the filter to the given metric.
	Apply(in ...Metric) []Metric
}

// StreamingProcessor is a processor that can take in a stream of messages
type StreamingProcessor interface {
	PluginDescriber

	// Start is called once when the plugin starts; it is only called once per
	// plugin instance, and never in parallel.
	// Start should return once it is ready to receive metrics.
	// The passed in accumulator is the same as the one passed to Add(), so you
	// can choose to save it in the plugin, or use the one received from Add().
	Start(acc Accumulator) error

	// Add is called for each metric to be processed. The Add() function does not
	// need to wait for the metric to be processed before returning, and it may
	// be acceptable to let background goroutine(s) handle the processing if you
	// have slow processing you need to do in parallel.
	// Keep in mind Add() should not spawn unbounded goroutines, so you may need
	// to use a semaphore or pool of workers (eg: reverse_dns plugin does this)
	// Metrics you don't want to pass downstream should have metric.Drop() called,
	// rather than simply omitting the acc.AddMetric() call
	Add(metric Metric, acc Accumulator) error

	// Stop gives you an opportunity to gracefully shut down the processor.
	// Once Stop() is called, Add() will not be called any more. If you are using
	// goroutines, you should wait for any in-progress metrics to be processed
	// before returning from Stop().
	// When stop returns, you should no longer be writing metrics to the
	// accumulator.
	Stop() error
}
