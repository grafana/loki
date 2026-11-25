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

// SampleIterator turns the rows of the batches of a [LogReader] into samples. It applies the LogQL
// sample extractor to each row and emits at most one sample per row.
//
// The samples that the iterator emits own their data.
//
// Next, At, Labels, StreamHash and Close are for a single goroutine. Err is safe to call from
// another one.
//
// Sample.Hash is left zero, so nothing deduplicates these samples. That rests on a precondition
// the caller must uphold and nothing here checks: the data-object tier is disjoint in time from
// the ingester and chunk tiers. A data object is already deduplicated internally. Leaving the
// hash zero is what allows the read to skip the message column for a count or a rate, whose
// samples could not be hashed anyway.
//
// The read returns no line when no stage reads it, and the extractor then gets a nil line. The
// extractor must then come from the query that [ProjectionPlan.UnpushedExpr] returns, or from the
// whole query when that is nil.
//
// It inherits the reader's lack of order (see [LogReader]). A consumer that needs
// timestamp order must sort.
type SampleIterator struct {
	reader    rowsBatchReader
	extractor syntax.SampleExtractor

	// closed is true after Close. Next then returns false.
	closed bool

	// rows iterates over the rows of the batch that Next is reading. It owns no batch before the
	// first batch and after the last one. It is a value, so a batch costs no allocation.
	rows rowsBatchIterator

	// currExtractor is the extractor of the stream of the current run, and hasCurrExtractor reports
	// whether there is one. currStreamHash and currStreamLabels identify that stream.
	//
	// Next stops at a sample without moving to the next row, so currStreamHash is also the stream
	// of the sample at the current position.
	currExtractor    logqllog.StreamSampleExtractor
	hasCurrExtractor bool
	currStreamHash   uint64
	currStreamLabels labels.Labels

	// lastResultLabels and lastResultLabelsString are the previous sample's labels and their
	// rendering.
	lastResultLabels       logqllog.LabelsResult
	lastResultLabelsString string

	// currSample and currSampleLabels are the sample at the current position, and hasCurrSample
	// reports whether there is one.
	currSample       logproto.Sample
	currSampleLabels string
	hasCurrSample    bool
}

// NewSampleIterator returns an iterator over the batches of reader. The caller must call Close.
//
// Outside this package, only a [*LogReader] or a type that embeds it can be the reader, because
// rowsBatchReader has an unexported method.
func NewSampleIterator(reader rowsBatchReader, extractor syntax.SampleExtractor) *SampleIterator {
	return &SampleIterator{reader: reader, extractor: extractor}
}

func (it *SampleIterator) Next() bool {
	// Close released the batch and closed the reader, so there is nothing left to read.
	if it.closed {
		return false
	}

	for {
		for it.rows.Next() {
			currRow := it.rows.At()
			// Initialise the extractor once for each run of rows of one stream, and when there is none yet.
			if !it.hasCurrExtractor || currRow.startsRun {
				it.initExtractorFor(currRow.stream)
			}

			// currRow.line is a view into the memory of the batch. The release of the batch lets the next
			// read overwrite it, so Process must not keep it after it returns.
			sample, ok := it.currExtractor.Process(currRow.timestamp, currRow.line, currRow.metadata)
			if !ok {
				continue // dropped by the pipeline, for instance by a label filter
			}

			// Sample.Hash stays zero, which turns off the merge's deduplication. See [SampleIterator].
			it.currSample = logproto.Sample{Timestamp: currRow.timestamp, Value: sample.Value}
			it.currSampleLabels = it.labelsString(sample.Labels)
			it.hasCurrSample = true
			return true
		}

		// The batch has no row left, or the iterator has no batch yet. Close releases the batch before
		// the reader gives the next one, so the scans can reuse its memory.
		it.rows.Close()
		batch := it.reader.nextRowsBatch()
		if batch == nil {
			it.hasCurrSample = false
			return false
		}
		it.rows = newRowsBatchIterator(batch)
	}
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

// initExtractorFor makes the stream extractor of a stream the current one, and keeps the current
// extractor while the stream does not change. The labels.Equal check guards the reuse, so a
// stream-hash collision cannot apply one stream's extractor to another's lines.
//
// Keeping the current extractor pays off when the first run of a batch continues the stream of the
// last run of the previous batch. Each run holds one stream, so the iterator calls this method once
// for each run. The reuse is only an optimisation: logs.SortTimestampDESC interleaves streams, and
// the labels.Equal check keeps it correct under any layout.
func (it *SampleIterator) initExtractorFor(stream objectStream) {
	if it.hasCurrExtractor && stream.streamHash == it.currStreamHash && labels.Equal(stream.streamLabels, it.currStreamLabels) {
		return
	}

	it.currExtractor = it.extractor.ForStream(labels.NewBuilder(stream.streamLabels).Del(model.MetricNameLabel).Labels())
	it.currStreamHash = stream.streamHash
	it.currStreamLabels = stream.streamLabels
	it.hasCurrExtractor = true
}

func (it *SampleIterator) At() logproto.Sample {
	if !it.hasCurrSample {
		return logproto.Sample{}
	}
	return it.currSample
}

func (it *SampleIterator) Labels() string {
	if !it.hasCurrSample {
		return ""
	}
	return it.currSampleLabels
}

// StreamHash returns the stream's labels.StableHash, matching what the chunk store's
// stream-first path reports.
func (it *SampleIterator) StreamHash() uint64 {
	if !it.hasCurrSample {
		return 0
	}
	return it.currStreamHash
}

func (it *SampleIterator) Err() error { return it.reader.Err() }

// Close releases the batch that the iterator is reading, and closes the reader. After Close, Next
// returns false.
func (it *SampleIterator) Close() error {
	it.rows.Close()
	it.closed = true
	it.hasCurrSample = false
	return it.reader.Close()
}
