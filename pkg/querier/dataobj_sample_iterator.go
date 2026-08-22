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

	// The logs section is sorted by stream, so records arrive in stream-clustered runs. A single
	// last-stream extractor cached entry serves the majority of log lines processed.
	lastStreamFingerprint uint64
	lastStreamsLabels     labels.Labels
	lastExtractor         logqllog.StreamSampleExtractor
	hasLastExtractor      bool

	// lastLabels and lastLabelString cache the rendered label string. Process returns a LabelsResult
	// whose String is memoized but reached through an interface; while the same result recurs — every
	// line of a constant-label stream, or a repeated grouping value — reuse the string instead of the
	// per-line interface call. LabelsResult is a pointer, so the comparison is identity.
	lastLabels      logqllog.LabelsResult
	lastLabelString string

	// curr is the sample at the current position. Process yields at most one sample per line, so no
	// pending slice is needed. hasCurr is false before the first Next and once the iterator is exhausted.
	curr    outSample
	currFP  uint64
	hasCurr bool
}

func newDataObjSampleIterator(reader dataObjRecordReader, extractor syntax.SampleExtractor) *dataObjSampleIterator {
	return &dataObjSampleIterator{reader: reader, extractor: extractor}
}

func (it *dataObjSampleIterator) Next() bool {
	for it.reader.Next() {
		rec := it.reader.At()
		se := it.streamExtractorFor(rec.fingerprint, rec.streamLabels)
		es, ok := se.Process(rec.timestamp, rec.line, rec.metadata)
		if !ok {
			continue // dropped by the pipeline (e.g. a line filter); try the next record
		}

		it.currFP = rec.fingerprint
		it.curr = outSample{
			sample: logproto.Sample{Timestamp: rec.timestamp, Value: es.Value},
			labels: it.labelString(es.Labels),
		}
		it.hasCurr = true
		return true
	}

	it.hasCurr = false
	return false
}

// labelString renders a Process result's labels, reusing the cached string while the same LabelsResult
// recurs. LabelsResult is a pointer, so the comparison is identity, not a value compare.
func (it *dataObjSampleIterator) labelString(lr logqllog.LabelsResult) string {
	if lr != it.lastLabels {
		it.lastLabels = lr
		it.lastLabelString = lr.String()
	}
	return it.lastLabelString
}

func (it *dataObjSampleIterator) streamExtractorFor(streamFingerprint uint64, streamLabels labels.Labels) logqllog.StreamSampleExtractor {
	// Check if the last cached extractor is still valid. The labels.Equal() check guards from fingerprint
	// collisions.
	if it.hasLastExtractor && streamFingerprint == it.lastStreamFingerprint && labels.Equal(streamLabels, it.lastStreamsLabels) {
		return it.lastExtractor
	}

	lbls := labels.NewBuilder(streamLabels).Del(model.MetricNameLabel).Labels()
	se := it.extractor.ForStream(lbls)
	it.lastStreamFingerprint, it.lastStreamsLabels, it.lastExtractor, it.hasLastExtractor = streamFingerprint, streamLabels, se, true
	return se
}

func (it *dataObjSampleIterator) At() logproto.Sample {
	if !it.hasCurr {
		return logproto.Sample{}
	}
	return it.curr.sample
}

func (it *dataObjSampleIterator) Labels() string {
	if !it.hasCurr {
		return ""
	}
	return it.curr.labels
}

func (it *dataObjSampleIterator) StreamHash() uint64 { return it.currFP }

func (it *dataObjSampleIterator) Err() error { return it.reader.Err() }

func (it *dataObjSampleIterator) Close() error { return it.reader.Close() }
