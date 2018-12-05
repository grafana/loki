package main

import (
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

const tailIteratorIncrement = 10 * time.Second

func tailQuery() iter.EntryIterator {
	return &tailIterator{
		from: time.Now().Add(-tailIteratorIncrement),
	}
}

type tailIterator struct {
	from time.Time
	err  error
	iter.EntryIterator
}

func (t *tailIterator) Next() bool {
	for t.EntryIterator == nil || !t.EntryIterator.Next() {
		through, now := t.from.Add(tailIteratorIncrement), time.Now()
		if through.After(now) {
			time.Sleep(through.Sub(now))
		}

		resp, err := query(t.from, through, logproto.FORWARD)
		if err != nil {
			t.err = err
			return false
		}

		// We store the through time such that if we don't see any entries, we will
		// still make forward progress. This is overwritten by any entries we might
		// see to ensure pagination works.
		t.from = through
		t.EntryIterator = iter.NewQueryResponseIterator(resp, logproto.FORWARD)
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
