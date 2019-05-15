package querier

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

// tailIteratorIncrement is for defining size of time window we want to query entries for
const tailIteratorIncrement = 10 * time.Second

// delayQuerying is for delaying querying of logs for specified seconds to not miss any late entries
const delayQuerying = 10 * time.Second

func (q *Querier) tailQuery(ctx context.Context, queryRequest *logproto.QueryRequest) iter.EntryIterator {
	queryRequest.Start = time.Now().Add(-(tailIteratorIncrement + delayQuerying))
	queryRequest.Direction = logproto.FORWARD

	return &tailIterator{
		queryRequest: queryRequest,
		querier:      q,
		ctx:          ctx,
	}
}

type tailIterator struct {
	queryRequest  *logproto.QueryRequest
	ctx           context.Context
	querier       *Querier
	entryIterator iter.EntryIterator
	err           error
}

func (t *tailIterator) Next() bool {
	var err error
	var now time.Time

	for t.entryIterator == nil || !t.entryIterator.Next() {
		t.queryRequest.End, now = t.queryRequest.Start.Add(tailIteratorIncrement), time.Now()
		if t.queryRequest.End.After(now.Add(-delayQuerying)) {
			time.Sleep(t.queryRequest.End.Sub(now.Add(-delayQuerying)))
		}

		t.entryIterator, err = t.query()
		if err != nil {
			t.err = err
			return false
		}

		// We store the through time such that if we don't see any entries, we will
		// still make forward progress. This is overwritten by any entries we might
		// see to ensure pagination works.
		t.queryRequest.Start = t.queryRequest.End
	}

	return true
}

func (t *tailIterator) Entry() logproto.Entry {
	entry := t.entryIterator.Entry()
	t.queryRequest.Start = entry.Timestamp.Add(1 * time.Nanosecond)
	return entry
}

func (t *tailIterator) Error() error {
	return t.err
}

func (t *tailIterator) Labels() string {
	return t.entryIterator.Labels()
}

func (t *tailIterator) Close() error {
	return t.entryIterator.Close()
}

func (t *tailIterator) query() (iter.EntryIterator, error) {
	ingesterIterators, err := t.querier.queryIngesters(t.ctx, t.queryRequest)
	if err != nil {
		return nil, err
	}

	chunkStoreIterators, err := t.querier.queryStore(t.ctx, t.queryRequest)
	if err != nil {
		return nil, err
	}

	iterators := append(ingesterIterators, chunkStoreIterators)
	return iter.NewHeapIterator(iterators, t.queryRequest.Direction), nil
}
