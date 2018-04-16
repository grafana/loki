package loki

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/apache/thrift/lib/go/thrift"
	"github.com/openzipkin/zipkin-go-opentracing/thrift/gen-go/zipkincore"
)

// Want to be able to support a service doing 100 QPS with a 15s scrape interval
var globalCollector = NewCollector(15 * 100)

type Collector struct {
	mtx      sync.Mutex
	traceIDs map[int64]int // map from trace ID to index in traces
	traces   []trace
	next     int
	length   int
}

type trace struct {
	traceID int64
	spans   []*zipkincore.Span
}

func NewCollector(capacity int) *Collector {
	return &Collector{
		traceIDs: make(map[int64]int, capacity),
		traces:   make([]trace, capacity, capacity),
		next:     0,
		length:   0,
	}
}

func (c *Collector) Collect(span *zipkincore.Span) error {
	if span == nil {
		return fmt.Errorf("cannot collect nil span")
	}

	c.mtx.Lock()
	defer c.mtx.Unlock()

	traceID := span.GetTraceID()
	idx, ok := c.traceIDs[traceID]
	if !ok {
		// Pick a slot in c.spans for this trace
		idx = c.next
		c.next++
		c.next %= cap(c.traces) // wrap

		// If the slot it occupied, we'll need to clear the trace ID index,
		// otherwise we'll need to number of traces.
		if c.length == cap(c.traces) {
			delete(c.traceIDs, c.traces[idx].traceID)
		} else {
			c.length++
		}

		// Initialise said slot.
		c.traceIDs[traceID] = idx
		c.traces[idx].traceID = traceID
		c.traces[idx].spans = c.traces[idx].spans[:0]
	}

	c.traces[idx].spans = append(c.traces[idx].spans, span)
	return nil
}

func (*Collector) Close() error {
	return nil
}

func (c *Collector) gather() []*zipkincore.Span {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	spans := make([]*zipkincore.Span, 0, c.length)
	i, count := c.next-c.length, 0
	if i < 0 {
		i = cap(c.traces) + i
	}
	for count < c.length {
		i %= cap(c.traces)
		spans = append(spans, c.traces[i].spans...)
		delete(c.traceIDs, c.traces[i].traceID)
		i++
		count++
	}
	c.length = 0
	if len(c.traceIDs) != 0 {
		panic("didn't clear all trace ids")
	}
	return spans
}

func (c *Collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	spans := c.gather()
	if err := WriteSpans(spans, w); err != nil {
		log.Printf("error writing spans: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func WriteSpans(spans []*zipkincore.Span, w io.Writer) error {
	transport := thrift.NewStreamTransportW(w)
	protocol := thrift.NewTCompactProtocol(transport)

	if err := protocol.WriteListBegin(thrift.STRUCT, len(spans)); err != nil {
		return err
	}
	for _, span := range spans {
		if err := span.Write(protocol); err != nil {
			return err
		}
	}
	if err := protocol.WriteListEnd(); err != nil {
		return err
	}
	return protocol.Flush()
}

func ReadSpans(r io.Reader) ([]*zipkincore.Span, error) {
	transport := thrift.NewStreamTransportR(r)
	protocol := thrift.NewTCompactProtocol(transport)
	ttype, size, err := protocol.ReadListBegin()
	if err != nil {
		return nil, err
	}
	spans := make([]*zipkincore.Span, 0, size)
	if ttype != thrift.STRUCT {
		return nil, fmt.Errorf("unexpected type: %v", ttype)
	}
	for i := 0; i < size; i++ {
		span := zipkincore.NewSpan()
		if err := span.Read(protocol); err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	return spans, protocol.ReadListEnd()
}

func Handler() http.Handler {
	return globalCollector
}
