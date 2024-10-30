package chunkenc

import (
	"context"
	"sort"
	"strconv"

	"github.com/cespare/xxhash/v2"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/prometheus/prometheus/model/labels"
)

func newMultiExtractorSampleIterator(ctx context.Context, pool compression.ReaderPool, b []byte, format byte, extractors []log.StreamSampleExtractor, symbolizer *symbolizer) iter.SampleIterator {
	return &multiExtractorSampleBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b, format, symbolizer),
		extractors:       extractors,
		stats:            stats.FromContext(ctx),
	}
}

type multiExtractorSampleBufferedIterator struct {
	*bufferedIterator

	extractors []log.StreamSampleExtractor
	stats      *stats.Context

	cur            []logproto.Sample
	currLabels     []log.LabelsResult
	currBaseLabels []log.LabelsResult
}

func (e *multiExtractorSampleBufferedIterator) Next() bool {
	if len(e.cur) > 0 {
		e.cur = e.cur[1:]
		e.currLabels = e.currLabels[1:]
		e.currBaseLabels = e.currBaseLabels[1:]

		return true
	}

	for e.bufferedIterator.Next() {
		for i, extractor := range e.extractors {
			val, lbls, ok := extractor.Process(e.currTs, e.currLine, e.currStructuredMetadata...)
			if !ok {
				continue
			}

			e.stats.AddPostFilterLines(1)

			streamLbls := lbls.Stream()
			streamLbls = append(streamLbls, labels.Label{
				Name:  "__variant__",
				Value: strconv.FormatInt(int64(i), 10),
			})

			builder := log.NewBaseLabelsBuilder().ForLabels(streamLbls, streamLbls.Hash())
			builder.Add(log.StructuredMetadataLabel, lbls.StructuredMetadata()...)
			builder.Add(log.ParsedLabel, lbls.Parsed()...)
			e.currLabels = append(e.currLabels, builder.LabelsResult())

      //TODO: is it enough to add __variant__ to result labels? Do the base labels need it to?
			e.currBaseLabels = append(e.currBaseLabels, extractor.BaseLabels())
			e.cur = append(e.cur, logproto.Sample{
				Value:     val,
				Hash:      xxhash.Sum64(e.currLine),
				Timestamp: e.currTs,
			})

			return true
		}
	}
	return false
}

func flattenLabels(buf labels.Labels, many ...labels.Labels) labels.Labels {
	var size int
	for _, lbls := range many {
		size += len(lbls)
	}

	if buf == nil || cap(buf) < size {
		buf = make(labels.Labels, 0, size)
	} else {
		buf = buf[:0]
	}

	for _, lbls := range many {
		buf = append(buf, lbls...)
	}
	sort.Sort(buf)
	return buf
}

func (e *multiExtractorSampleBufferedIterator) Close() error {
	for _, extractor := range e.extractors {
		if extractor.ReferencedStructuredMetadata() {
			e.stats.SetQueryReferencedStructuredMetadata()
		}
	}

	return e.bufferedIterator.Close()
}

func (e *multiExtractorSampleBufferedIterator) Labels() string { return e.currLabels[0].String() }

func (e *multiExtractorSampleBufferedIterator) StreamHash() uint64 { return e.currBaseLabels[0].Hash() }

func (e *multiExtractorSampleBufferedIterator) At() logproto.Sample { return e.cur[0] }
