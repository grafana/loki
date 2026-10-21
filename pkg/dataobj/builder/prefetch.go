package builder

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

// prefetchBytes is the most bytes of record values that the processor reads
// ahead of the record that it processes.
//
// kgo fetches from a broker only when no earlier fetch from that broker waits
// to be polled. Without a buffer, the consumer polls only when the processor
// has taken every record of the previous fetch, so each slow fetch stops the
// processor. 200 MB is about four fetches of the reader client, which caps a
// partition fetch at 50 MB.
const prefetchBytes = 200 << 20

// bufferRecords moves records from in to out in order. It reads from in while
// it holds less than maxBytes of record values, so that the consumer keeps
// polling while the reader of out is busy. A record larger than maxBytes
// passes when bufferRecords holds nothing else. buffered reports the bytes
// that bufferRecords holds.
//
// When ctx is done, or when bufferRecords reads that in is closed, it closes
// out and drops the records it holds. At the byte limit it does not read from
// in, so it sees in closed only after the reader of out takes a record. The
// builder has not committed the offsets of the dropped records, so it reads
// them again after a restart.
func bufferRecords(ctx context.Context, in <-chan *kgo.Record, out chan<- *kgo.Record, maxBytes int, buffered prometheus.Gauge) {
	defer close(out)
	defer buffered.Set(0)

	var (
		queue []*kgo.Record
		size  int
	)
	for {
		var (
			readFrom <-chan *kgo.Record
			sendTo   chan<- *kgo.Record
			next     *kgo.Record
		)
		if size < maxBytes {
			readFrom = in
		}
		if len(queue) > 0 {
			sendTo, next = out, queue[0]
		}

		select {
		case <-ctx.Done():
			return
		case rec, ok := <-readFrom:
			if !ok {
				return
			}
			queue = append(queue, rec)
			size += len(rec.Value)
		case sendTo <- next:
			queue[0] = nil
			queue = queue[1:]
			size -= len(next.Value)
		}
		buffered.Set(float64(size))
	}
}
