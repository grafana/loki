package aggregation

import (
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// entrySize estimates the size in bytes of an entry. It is the length of the
// line, the size of the timestamp, the size of the labels of the stream and the
// size of the structured metadata. The labels of the stream are counted for
// every entry even though the request has them once per stream, which makes up
// for the encoding overhead of an entry that is not counted.
func entrySize(stream string, e logproto.Entry) int {
	const timestampSize = 8
	size := len(e.Line) + timestampSize + len(stream)
	for _, l := range e.StructuredMetadata {
		size += len(l.Name) + len(l.Value)
	}
	return size
}

type batch struct {
	// count is the number of entries in the batch, across all streams. It is
	// used for metrics.
	count int
	// size is the estimated size of the batch, which is the sum of entrySize
	// for each entry. A new batch is started when adding an entry would take
	// it over the maximum batch size.
	size int
	// entries are the entries of the batch, keyed by the labels of the stream.
	entries map[string][]logproto.Entry
}

func (b *batch) add(stream string, e logproto.Entry, size int) {
	b.entries[stream] = append(b.entries[stream], e)
	b.size += size
	b.count++
}

// buildBatches drains the buffered entries and splits them into as many batches
// as needed to keep the estimated size of each below the maximum batch size.
func (p *Push) buildBatches() []batch {
	entries := p.entries.reset()
	if len(entries) == 0 {
		return nil
	}

	maxBatchSize := p.maxBatchSize
	if maxBatchSize <= 0 {
		maxBatchSize = defaultMaxBatchSize
	}

	var (
		batches  []batch
		services = make(map[string]struct{})
	)
	for _, e := range entries {
		stream := e.labels.String()
		services[stream] = struct{}{}
		entry := logproto.Entry{
			Timestamp:          e.ts,
			Line:               e.entry,
			StructuredMetadata: e.structuredMetadata,
		}

		size := entrySize(stream, entry)
		// Start a new batch if there is none, or if the entry would take the last
		// one over the maximum batch size. An entry that is larger than the
		// maximum batch size is therefore in a batch of its own.
		if len(batches) == 0 || batches[len(batches)-1].size+size > maxBatchSize {
			batches = append(batches, batch{entries: make(map[string][]logproto.Entry)})
		}
		tmp := &batches[len(batches)-1]
		tmp.add(stream, entry, size)
	}

	// limit the number of services to log to 1000
	serviceLimit := len(services)
	if serviceLimit > 1000 {
		serviceLimit = 1000
	}
	p.metrics.servicesTracked.WithLabelValues(p.tenantID).Set(float64(serviceLimit))

	level.Debug(p.logger).Log(
		"msg", "built aggregation batches",
		"num_service", len(services),
		"num_batches", len(batches),
		"num_entries", len(entries),
	)

	return batches
}

// encode creates the snappy compressed protobuf of the batch to send to Loki.
// It returns nil if none of the streams in the batch have valid labels, and an
// error if the push request cannot be marshaled.
func (p *Push) encode(b batch) ([]byte, error) {
	streams := make([]logproto.Stream, 0, len(b.entries))
	for s, entries := range b.entries {
		lbls, err := syntax.ParseLabels(s)
		if err != nil {
			continue
		}

		streams = append(streams, logproto.Stream{
			Labels:  s,
			Entries: entries,
			Hash:    labels.StableHash(lbls),
		})
	}

	if len(streams) == 0 {
		return nil, nil
	}

	req := &logproto.PushRequest{
		Streams: streams,
	}
	body, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload to json: %w", err)
	}

	p.metrics.streamsPerPush.WithLabelValues(p.tenantID).Observe(float64(len(streams)))
	p.metrics.entriesPerPush.WithLabelValues(p.tenantID).Observe(float64(b.count))

	return snappy.Encode(nil, body), nil
}
