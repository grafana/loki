package distributor

// Tee imlpementations can duplicate the log streams to another endpoint.
type Tee interface {
	Duplicate([]KeyedStream)
}
