package ring

import "context"

// FlushTransferer controls the shutdown of an instance in the ring.
type FlushTransferer interface {
	StopIncomingRequests()
	Flush()
	TransferOut(ctx context.Context) error
}

// NoopFlushTransferer is a FlushTransferer which does nothing and can
// be used in cases we don't need one
type NoopFlushTransferer struct{}

// NewNoopFlushTransferer makes a new NoopFlushTransferer
func NewNoopFlushTransferer() *NoopFlushTransferer {
	return &NoopFlushTransferer{}
}

// StopIncomingRequests is a noop
func (t *NoopFlushTransferer) StopIncomingRequests() {}

// Flush is a noop
func (t *NoopFlushTransferer) Flush() {}

// TransferOut is a noop
func (t *NoopFlushTransferer) TransferOut(ctx context.Context) error {
	return nil
}
