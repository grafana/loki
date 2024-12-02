package blockbuilder

// Transport interface defines the methods for communication between scheduler and workers.
type Transport interface {
	Send(message interface{}) error
	Receive() (interface{}, error)
}

type unimplementedTransport struct{}

func (t *unimplementedTransport) Send(message interface{}) error {
	panic("unimplemented")
}

func (t *unimplementedTransport) Receive() (interface{}, error) {
	panic("unimplemented")
}

var _ Transport = &GRPCTransport{}

// GRPCTransport is the default implementation of the Transport interface using gRPC.
type GRPCTransport struct {
	// Add necessary fields here
	unimplementedTransport
}

// NewGRPCTransport creates a new GRPCTransport instance.
func NewGRPCTransport() *GRPCTransport {
	return &GRPCTransport{}
}
