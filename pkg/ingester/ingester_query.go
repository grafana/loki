package ingester

import "context"

type IngesterQuery struct {
	Id       uint32
	instance *instance
	loopAck  chan struct{}
}

func NewIngesterQuery(id uint32, instance *instance) *IngesterQuery {
	ret := &IngesterQuery{
		Id:       id,
		instance: instance,
		loopAck:  make(chan struct{}, 30),
	}

	// max 20 in flight
	for i := 0; i < 20; i++ {
		ret.loopAck <- struct{}{}
	}

	return ret
}

func (q *IngesterQuery) End() {
	q.instance.queryMtx.Lock()
	delete(q.instance.queries, q.Id)
	q.instance.queryMtx.Unlock()
}

func (q *IngesterQuery) BackpressureWait(ctx context.Context) {

	select {
	case <-ctx.Done():
		// The context is over, stop processing results
		return
	case <-q.loopAck:
		// Process the results received
	}
}

func (q *IngesterQuery) ReleaseAck() {
	q.loopAck <- struct{}{}
}
