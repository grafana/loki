package distributor

// Tee implementations can duplicate the log streams to another endpoint.
type Tee interface {
	Duplicate(tenant string, streams []KeyedStream)
}

// todo finish
func MultiTee(tees ...Tee) Tee {
	return multiTee{tees}
}

type multiTee struct {
	tees []Tee
}

func (m multiTee) Duplicate(tenant string, streams []KeyedStream) {
	for _, tee := range m.tees {
		tee.Duplicate(tenant, streams)
	}
}
