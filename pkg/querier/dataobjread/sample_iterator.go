package dataobjread

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqllog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var _ iter.SampleIterator = (*SampleIterator)(nil)

// SampleIterator turns the decoded log lines of a [LogReader] into samples. It
// applies the LogQL sample extractor to each line and emits at most one sample per line.
//
// Sample.Hash is left zero, so nothing deduplicates these samples. That rests on a precondition
// the caller must uphold and nothing here checks: the data-object tier is disjoint in time from
// the ingester and chunk tiers. A data object is already deduplicated internally. Leaving the
// hash zero is what allows the read to skip the message column for a count or a rate, whose
// samples could not be hashed anyway.
//
// It inherits the reader's lack of order (see [LogReader]). A consumer that needs
// timestamp order must sort.
type SampleIterator struct {
	reader    recordReader
	extractor syntax.SampleExtractor

	// lastStreamHash and lastLabels identify the stream the previous record belonged to, and
	// lastExtractor is that stream's extractor. hasLastExtractor reports whether there was one.
	lastStreamHash   uint64
	lastLabels       labels.Labels
	lastExtractor    logqllog.StreamSampleExtractor
	hasLastExtractor bool

	// lastResultLabels and lastResultLabelsString are the previous sample's labels and their
	// rendering.
	lastResultLabels       logqllog.LabelsResult
	lastResultLabelsString string

	// currSample, currLabels and currStreamHash are the sample at the current position, and hasCurr
	// reports whether there is one.
	currSample     logproto.Sample
	currLabels     string
	currStreamHash uint64
	hasCurr        bool
}

func NewSampleIterator(reader recordReader, extractor syntax.SampleExtractor) *SampleIterator {
	return &SampleIterator{reader: reader, extractor: extractor}
}

func (it *SampleIterator) Next() bool {
	for it.reader.Next() {
		record := it.reader.At()
		extractor := it.extractorFor(record.streamHash, record.streamLabels)
		sample, ok := extractor.Process(record.timestamp, record.line, record.metadata)
		if !ok {
			continue // dropped by the pipeline, for instance by a line filter
		}

		it.currSample = logproto.Sample{Timestamp: record.timestamp, Value: sample.Value}
		it.currLabels = it.labelsString(sample.Labels)
		it.currStreamHash = record.streamHash
		it.hasCurr = true
		return true
	}

	it.hasCurr = false
	return false
}

// labelsString renders a Process result's labels, reusing the cached string while the same
// labels recur. That saves one interface call per line of a constant-label stream, or per
// repeated grouping value.
func (it *SampleIterator) labelsString(resultLabels logqllog.LabelsResult) string {
	if resultLabels != it.lastResultLabels {
		it.lastResultLabels = resultLabels
		it.lastResultLabelsString = resultLabels.String()
	}
	return it.lastResultLabelsString
}

// extractorFor returns the stream extractor for a stream, reusing the last one while the stream
// does not change. The labels.Equal check guards the reuse, so a stream-hash collision cannot
// apply one stream's extractor to another's lines.
//
// Caching one is worth it because the layouts the production builder writes sort stream hash and
// stream ID ahead of timestamp, so rows arrive in contiguous per-stream runs. It is only an
// optimisation: logs.SortTimestampDESC interleaves streams, and the guard keeps the cache correct
// under any layout.
func (it *SampleIterator) extractorFor(streamHash uint64, streamLabels labels.Labels) logqllog.StreamSampleExtractor {
	if it.hasLastExtractor && streamHash == it.lastStreamHash && labels.Equal(streamLabels, it.lastLabels) {
		return it.lastExtractor
	}

	extractor := it.extractor.ForStream(labels.NewBuilder(streamLabels).Del(model.MetricNameLabel).Labels())
	it.lastStreamHash, it.lastLabels, it.lastExtractor, it.hasLastExtractor = streamHash, streamLabels, extractor, true
	return extractor
}

func (it *SampleIterator) At() logproto.Sample {
	if !it.hasCurr {
		return logproto.Sample{}
	}
	return it.currSample
}

func (it *SampleIterator) Labels() string {
	if !it.hasCurr {
		return ""
	}
	return it.currLabels
}

// StreamHash returns the stream's labels.StableHash, matching what the chunk store's
// stream-first path reports.
func (it *SampleIterator) StreamHash() uint64 {
	if !it.hasCurr {
		return 0
	}
	return it.currStreamHash
}

func (it *SampleIterator) Err() error { return it.reader.Err() }

func (it *SampleIterator) Close() error { return it.reader.Close() }
