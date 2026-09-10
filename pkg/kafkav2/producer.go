package kafkav2

import (
	"context"
	"sync/atomic" //lint:ignore faillint we use new atomic types from sync/atomic.

	"github.com/twmb/franz-go/pkg/kgo"
)

type Producer interface {
	Produce(context.Context, *kgo.Record, func(*kgo.Record, error))
	TryProduce(context.Context, *kgo.Record, func(*kgo.Record, error))
}

// A NoCancelProducer wraps a [kgo.Client] but unlike the wrapped client,
// cancelation of a context does not risk cancelation of all buffered records
// for the produced partitions. From the franz-go docs:
//
//	Once a record is buffered into a batch, it can be canceled in three ways:
//	canceling the context, the record timing out, or hitting the maximum
//	retries. If any of these conditions are hit and it is currently safe to
//	fail records, all buffered records for the relevant partition are failed.
//	Only the first record's context in a batch is considered when determining
//	whether the batch should be canceled.
//
// For more information, see
// [https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Client.Produce].
type NoCancelProducer struct {
	client Producer
}

// NewNoCancelProducer returns a new NoCancelProducer.
func NewNoCancelProducer(client Producer) *NoCancelProducer {
	return &NoCancelProducer{client: client}
}

// Produce sends the Kafka record to its requested topic. It calls the optional
// promise when Kafka replies. The error is non-nil on failure. However, unlike
// [kgo], a canceled context for one record cannot cancel all other buffered
// records for the same partition; nor can a promise be called with a nil error
// after the context has been canceled. For this to work, we spawn a goroutine
// for each context canceled via [context.AfterFunc].
func (p *NoCancelProducer) Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	noCancelCtx := context.WithoutCancel(ctx)
	if promise == nil {
		p.client.Produce(noCancelCtx, r, nil)
		return
	}
	// Slow path: ensure promise is called when ctx is canceled.
	once := atomic.Int64{}
	stop := context.AfterFunc(ctx, func() {
		if once.CompareAndSwap(0, 1) {
			promise(r, ctx.Err())
		}
	})
	producePromise := func(r *kgo.Record, err error) {
		if once.CompareAndSwap(0, 1) {
			stop() // Don't spawn a G if context is canceled after [Produce] finishes.
			promise(r, err)
		}
	}
	p.client.Produce(noCancelCtx, r, producePromise)
}

// TryProduce has the same no cancel behavior as [Produce], but does not block
// when the buffer is full. It calls the promise with a [kgo.ErrMaxBuffered]
// error instead.
func (p *NoCancelProducer) TryProduce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	noCancelCtx := context.WithoutCancel(ctx)
	if promise == nil {
		p.client.TryProduce(noCancelCtx, r, nil)
		return
	}
	// Slow path: ensure promise is called when ctx is canceled.
	once := atomic.Int64{}
	stop := context.AfterFunc(ctx, func() {
		if once.CompareAndSwap(0, 1) {
			promise(r, ctx.Err())
		}
	})
	producePromise := func(r *kgo.Record, err error) {
		if once.CompareAndSwap(0, 1) {
			stop() // Don't spawn a G if context is canceled after [Produce] finishes.
			promise(r, err)
		}
	}
	p.client.TryProduce(noCancelCtx, r, producePromise)
}

// ProduceSync produces all Kafka records to their requested topics. It waits
// for all records to finish, or the context to be canceled, whichever happens
// first. If the context is canceled, all records are returned with an error
// "context canceled".
func (p *NoCancelProducer) ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	results := make(kgo.ProduceResults, 0, len(rs))
	if len(rs) == 0 {
		return results
	}
	noCancelCtx := context.WithoutCancel(ctx)
	// done is closed when resultsCh is closed.
	done := make(chan struct{})
	// resultsCh contains a [kgo.ProduceResult] for each record in rs.
	resultsCh := make(chan kgo.ProduceResult, len(rs))
	// rem counts the number of promises still to be called. It ensures that
	// resultsCh is closed once, and after all promises have been executed.
	rem := atomic.Int64{}
	rem.Add(int64(len(rs)))
	for _, r := range rs {
		p.client.Produce(noCancelCtx, r, func(r *kgo.Record, err error) {
			resultsCh <- kgo.ProduceResult{
				Record: r,
				Err:    err,
			}
			if rem.Add(-1) == 0 {
				// This is the last promise, so we must close resultsCh.
				close(resultsCh)
				close(done)
			}
		})
	}
	select {
	case <-ctx.Done():
		// ctx.Err() is not free on a [cancelCtx], call it once.
		ctxErr := ctx.Err()
		for _, r := range rs {
			results = append(results, kgo.ProduceResult{
				Record: r,
				Err:    ctxErr,
			})
		}
	case <-done:
		for res := range resultsCh {
			results = append(results, res)
		}
	}
	return results
}
