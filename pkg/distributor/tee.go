package distributor

// Tee implementations can duplicate the log streams to another endpoint.
type Tee interface {
	Duplicate(tenant string, streams []KeyedStream)
}
