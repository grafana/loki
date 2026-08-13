package querier

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqllog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var _ iter.SampleIterator = (*dataObjSampleIterator)(nil)

// outSample pairs an extracted sample with its rendered label set. logproto.Sample has no label field,
// so the labels ride alongside it here.
type outSample struct {
	sample logproto.Sample
	labels string
}

// dataObjSampleIterator turns the decoded log lines from a dataObjLogReader into samples for the
// range-vector evaluator. It applies the LogQL sample extractor to each line and emits at most one
// sample per line.
//
// Sample.Hash is left 0 (never deduplicated): routing guarantees the data-object tier is disjoint in
// time from the ingester and chunk tiers, so there is nothing to deduplicate across sources, and data
// objects are internally deduplicated. This is what lets the reader skip the message column for
// count/rate.
type dataObjSampleIterator struct {
	reader    dataObjRecordReader
	extractor syntax.SampleExtractor

	// streamExtractors caches the per-stream extractor by fingerprint, since records from different
	// streams can interleave across concurrently-scanned sections.
	streamExtractors map[uint64]logqllog.StreamSampleExtractor

	currFP      uint64
	currPending []outSample
	currPos     int
}

func newDataObjSampleIterator(reader dataObjRecordReader, extractor syntax.SampleExtractor) *dataObjSampleIterator {
	return &dataObjSampleIterator{
		reader:           reader,
		extractor:        extractor,
		streamExtractors: map[uint64]logqllog.StreamSampleExtractor{},
		currPos:          -1,
	}
}

func (it *dataObjSampleIterator) Next() bool {
	for {
		if it.currPos+1 < len(it.currPending) {
			it.currPos++
			return true
		}
		if !it.reader.Next() {
			return false
		}
		rec := it.reader.At()
		it.currFP = rec.fingerprint
		it.currPending = it.extract(rec)
		it.currPos = 0
		if len(it.currPending) > 0 {
			return true
		}
	}
}

func (it *dataObjSampleIterator) extract(rec dataObjLogRecord) []outSample {
	se := it.streamExtractorFor(rec.fingerprint, rec.streamLabels)
	out := it.currPending[:0] // reuse: the previous batch is fully consumed
	es, ok := se.Process(rec.timestamp, rec.line, rec.metadata)
	if !ok {
		return out
	}
	return append(out, outSample{
		sample: logproto.Sample{Timestamp: rec.timestamp, Value: es.Value},
		labels: es.Labels.String(),
	})
}

func (it *dataObjSampleIterator) streamExtractorFor(fp uint64, streamLabels labels.Labels) logqllog.StreamSampleExtractor {
	if se, ok := it.streamExtractors[fp]; ok {
		return se
	}
	lbls := labels.NewBuilder(streamLabels).Del(model.MetricNameLabel).Labels()
	se := it.extractor.ForStream(lbls)
	it.streamExtractors[fp] = se
	return se
}

func (it *dataObjSampleIterator) At() logproto.Sample {
	if it.currPos < 0 || it.currPos >= len(it.currPending) {
		return logproto.Sample{}
	}
	return it.currPending[it.currPos].sample
}

func (it *dataObjSampleIterator) Labels() string {
	if it.currPos < 0 || it.currPos >= len(it.currPending) {
		return ""
	}
	return it.currPending[it.currPos].labels
}

func (it *dataObjSampleIterator) StreamHash() uint64 { return it.currFP }

func (it *dataObjSampleIterator) Err() error { return it.reader.Err() }

func (it *dataObjSampleIterator) Close() error { return it.reader.Close() }
