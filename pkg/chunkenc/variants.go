package chunkenc

import (
	"context"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
)

func newMultiExtractorSampleIterator(
	ctx context.Context,
	pool compression.ReaderPool,
	b []byte,
	format byte,
	symbolizer *symbolizer,
	extractors ...log.StreamSampleExtractor,
) iter.SampleIterator {
	return &multiExtractorSampleBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b, format, symbolizer),
		extractors:       extractors,
		stats:            stats.FromContext(ctx),
	}
}

// TODO(twhitney): Once multi-variant queries have been validated,
// we should merge this into the regular sampledBufferedIterator.
type multiExtractorSampleBufferedIterator struct {
	*bufferedIterator

	extractors []log.StreamSampleExtractor
	stats      *stats.Context

	cur            []logproto.Sample
	currLabels     []log.LabelsResult
	currBaseLabels []log.LabelsResult
}

func (e *multiExtractorSampleBufferedIterator) Next() bool {
	if len(e.cur) > 1 {
		e.cur = e.cur[1:]
		e.currLabels = e.currLabels[1:]
		e.currBaseLabels = e.currBaseLabels[1:]

		return true
	}

	if len(e.cur) == 1 {
		e.cur = e.cur[:0]
		e.currLabels = e.currLabels[:0]
		e.currBaseLabels = e.currBaseLabels[:0]
	}

	for e.bufferedIterator.Next() {
		e.stats.AddPostFilterLines(1)

		for _, extractor := range e.extractors {
			samples, ok := extractor.Process(e.currTs, e.currLine, e.currStructuredMetadata)
			if !ok || len(samples) == 0 {
				continue
			}

			for _, sample := range samples {
				e.currLabels = append(e.currLabels, sample.Labels)
				e.currBaseLabels = append(e.currBaseLabels, extractor.BaseLabels())

				lblString := sample.Labels.String()
				e.cur = append(e.cur, logproto.Sample{
					Value:     sample.Value,
					Hash:      util.UniqueSampleHash(lblString, e.currLine),
					Timestamp: e.currTs,
				})
			}
		}

		// catch the case where no extractors were ok
		if len(e.cur) <= 1 {
			continue
		}

		return true
	}

	return false
}

func (e *multiExtractorSampleBufferedIterator) Labels() string { return e.currLabels[0].String() }

func (e *multiExtractorSampleBufferedIterator) StreamHash() uint64 { return e.currBaseLabels[0].Hash() }

func (e *multiExtractorSampleBufferedIterator) At() logproto.Sample {
	return e.cur[0]
}
