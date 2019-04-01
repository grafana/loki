package querier

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

// tailIteratorIncrement is for defining size of time window we want to query entries for
const tailIteratorIncrement = 10 * time.Second
// delay is for delaying querying of logs for specified seconds to not miss any late entries
const delay = 10 * time.Second

func (q *Querier) tailQuery(ctx context.Context, queryStr, regexp string) iter.EntryIterator {
	return &tailIterator{
		from:     time.Now().Add(-(tailIteratorIncrement+delay)),
		queryStr: queryStr,
		regexp:   regexp,
		querier:  q,
		ctx:      ctx,
	}
}

type tailIterator struct {
	queryStr string
	regexp   string
	from     time.Time
	err      error
	ctx      context.Context
	querier  *Querier
	iter.EntryIterator
}

func (t *tailIterator) Next() bool {
	var err error
	for t.EntryIterator == nil || !t.EntryIterator.Next() {
		through, now := t.from.Add(tailIteratorIncrement), time.Now()
		if through.After(now.Add(-delay)) {
			time.Sleep(through.Sub(now.Add(-delay)))
		}

		t.EntryIterator, err = t.query(t.from, through)
		if err != nil {
			t.err = err
			return false
		}

		// We store the through time such that if we don't see any entries, we will
		// still make forward progress. This is overwritten by any entries we might
		// see to ensure pagination works.
		t.from = through
	}

	return true
}

func (t *tailIterator) Entry() logproto.Entry {
	entry := t.EntryIterator.Entry()
	t.from = entry.Timestamp.Add(1 * time.Nanosecond)
	return entry
}

func (t *tailIterator) Error() error {
	return t.err
}

func (t *tailIterator) query(from, through time.Time) (iter.EntryIterator, error) {
	request := &logproto.QueryRequest{
		Query:     t.queryStr,
		Limit:     30,
		Start:     from,
		End:       through,
		Direction: logproto.FORWARD,
		Regex:     t.regexp,
	}

	ingesterIterators, err := t.querier.queryIngesters(t.ctx, request)
	if err != nil {
		return nil, err
	}

	chunkStoreIterators, err := t.querier.queryStore(t.ctx, request)
	if err != nil {
		return nil, err
	}

	iterators := append(chunkStoreIterators, ingesterIterators...)
	return iter.NewHeapIterator(iterators, request.Direction), nil
}
