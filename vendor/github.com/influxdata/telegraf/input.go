package telegraf

type Input interface {
	PluginDescriber

	// Gather takes in an accumulator and adds the metrics that the Input
	// gathers. This is called every agent.interval
	Gather(Accumulator) error
}

type ServiceInput interface {
	Input

	// Start the ServiceInput.  The Accumulator may be retained and used until
	// Stop returns.
	Start(Accumulator) error

	// Stop stops the services and closes any necessary channels and connections.
	// Metrics should not be written out to the accumulator once stop returns, so
	// Stop() should stop reading and wait for any in-flight metrics to write out
	// to the accumulator before returning.
	Stop()
}
