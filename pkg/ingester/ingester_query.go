package ingester

import (
	"context"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util/math"
)

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

// QuerierQueryServer is the GRPC server stream we use to send batch of entries.
type QuerierQueryServer interface {
	Context() context.Context
	Send(res *logproto.QueryResponse) error
}

func (q *IngesterQuery) SendBatches(ctx context.Context, i iter.EntryIterator, queryServer QuerierQueryServer, limit int32) error {
	stats := stats.FromContext(ctx)

	// send until the limit is reached.
	for limit != 0 && !isDone(ctx) {
		q.BackpressureWait(ctx)

		fetchSize := uint32(queryBatchSize)
		if limit > 0 {
			fetchSize = math.MinUint32(queryBatchSize, uint32(limit))
		}
		batch, batchSize, err := iter.ReadBatch(i, fetchSize)
		if err != nil {
			q.End()
			return err
		}

		if limit > 0 {
			limit -= int32(batchSize)
		}

		if len(batch.Streams) == 0 {
			q.End()
			return nil
		}

		stats.AddIngesterBatch(int64(batchSize))
		batch.Stats = stats.Ingester()
		batch.Id = q.Id

		if err := queryServer.Send(batch); err != nil {
			q.End()
			return err
		}
		stats.Reset()
	}

	q.End()
	return nil
}

func (q *IngesterQuery) SendSampleBatches(ctx context.Context, it iter.SampleIterator, queryServer logproto.Querier_QuerySampleServer) error {
	stats := stats.FromContext(ctx)
	for !isDone(ctx) {
		q.BackpressureWait(ctx)

		batch, size, err := iter.ReadSampleBatch(it, queryBatchSampleSize)
		if err != nil {
			q.End()
			return err
		}
		if len(batch.Series) == 0 {
			q.End()
			return nil
		}

		stats.AddIngesterBatch(int64(size))
		batch.Stats = stats.Ingester()
		batch.Id = q.Id

		if err := queryServer.Send(batch); err != nil {
			q.End()
			return err
		}

		stats.Reset()

	}
	q.End()
	return nil
}
