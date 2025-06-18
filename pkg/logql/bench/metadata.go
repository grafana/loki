package bench

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

var streamsPool = sync.Pool{
	New: func() any {
		streams := make([]streams.Stream, 1024)
		return &streams
	},
}

// SelectSeries implements querier.Store
func (s *ParquetQuerier) SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	/*
		 	logger := util_log.WithContext(ctx, s.logger)

			objects, err := s.objectsForTimeRange(ctx, req.Start, req.End, logger)
			if err != nil {
				return nil, err
			}

			if len(objects) == 0 {
				return nil, nil
			}

			shard, err := parseShards(req.Shards)
			if err != nil {
				return nil, err
			}

			var matchers []*labels.Matcher
			if req.Selector != "" {
				expr, err := req.LogSelector()
				if err != nil {
					return nil, err
				}
				matchers = expr.Matchers()
			}

			uniqueSeries := &sync.Map{}

			processor := newStreamProcessor(req.Start, req.End, matchers, objects, shard, logger)

			err = processor.ProcessParallel(ctx, func(h uint64, stream streams.Stream) {
				uniqueSeries.Store(h, labelsToSeriesIdentifier(stream.Labels))
			})
			if err != nil {
				return nil, err
			}
			var result []logproto.SeriesIdentifier

			// Convert sync.Map to slice
			uniqueSeries.Range(func(_, value interface{}) bool {
				if sid, ok := value.(logproto.SeriesIdentifier); ok {
					result = append(result, sid)
				}
				return true
			})
	*/return nil, nil
}

// LabelNamesForMetricName implements querier.Store
func (s *ParquetQuerier) LabelNamesForMetricName(ctx context.Context, _ string, from, through model.Time, _ string, matchers ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

// LabelValuesForMetricName implements querier.Store
func (s *ParquetQuerier) LabelValuesForMetricName(ctx context.Context, _ string, from, through model.Time, _ string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

// streamProcessor handles processing of unique series with custom collection logic
type streamProcessor struct {
	predicate  streams.RowPredicate
	seenSeries *sync.Map
	objects    []object
	shard      logql.Shard
	logger     log.Logger
}

/*
// newStreamProcessor creates a new streamProcessor with the given parameters

	func newStreamProcessor(start, end time.Time, matchers []*labels.Matcher, objects []object, shard logql.Shard, logger log.Logger) *streamProcessor {
		return &streamProcessor{
			predicate:  streamPredicate(matchers, start, end),
			seenSeries: &sync.Map{},
			objects:    objects,
			shard:      shard,
			logger:     logger,
		}
	}

// ProcessParallel processes series from multiple readers in parallel
// dataobj.Stream objects returned to onNewStream may be reused and must be deep copied for further use, including the stream.Labels keys and values.

	func (sp *streamProcessor) ProcessParallel(ctx context.Context, onNewStream func(uint64, streams.Stream)) error {
		start := time.Now()
		span := trace.SpanFromContext(ctx)
		span.AddEvent("processing streams", trace.WithAttributes(attribute.Int("total_readers", len(sp.objects))))
		level.Debug(sp.logger).Log("msg", "processing streams", "total_readers", len(sp.objects))

		g, ctx := errgroup.WithContext(ctx)
		var processedStreams atomic.Int64
		for _, obj := range sp.objects {
			g.Go(func() error {
				ctx, span := tracer.Start(ctx, "streamProcessor.processSingleReader")
				defer span.End()

				n, err := sp.processSingleReader(ctx, obj.File, onNewStream)
				if err != nil {
					return err
				}
				processedStreams.Add(n)
				return nil
			})
		}
		err := g.Wait()
		if err != nil {
			return err
		}

		level.Debug(sp.logger).Log("msg", "finished processing streams",
			"total_streams_processed", processedStreams.Load(),
			"duration", time.Since(start),
		)
		span.AddEvent("streamProcessor.ProcessParallel done", trace.WithAttributes(
			attribute.Int("total_streams_processed", int(processedStreams.Load())),
			attribute.String("duration", time.Since(start).String()),
		))

		return nil
	}

	func (sp *streamProcessor) processSingleReader(ctx context.Context, reader *streams.RowReader, onNewStream func(uint64, streams.Stream)) (int64, error) {
		var (
			streamsPtr = streamsPool.Get().(*[]streams.Stream)
			streams    = *streamsPtr
			buf        = make([]byte, 0, 1024)
			h          uint64
			processed  int64
		)

		defer streamsPool.Put(streamsPtr)

		for {
			n, err := reader.Read(ctx, streams)
			if err != nil && err != io.EOF {
				return processed, fmt.Errorf("failed to read streams: %w", err)
			}
			if n == 0 && err == io.EOF {
				break
			}
			for _, stream := range streams[:n] {
				h, buf = stream.Labels.HashWithoutLabels(buf, []string(nil)...)
				// Try to claim this hash first
				if _, seen := sp.seenSeries.LoadOrStore(h, nil); seen {
					continue
				}
				onNewStream(h, stream)
				processed++
			}
		}
		return processed, nil
	}
*/
func labelsToSeriesIdentifier(labels labels.Labels) logproto.SeriesIdentifier {
	series := make([]logproto.SeriesIdentifier_LabelsEntry, len(labels))
	for i, label := range labels {
		series[i] = logproto.SeriesIdentifier_LabelsEntry{
			Key:   label.Name,
			Value: label.Value,
		}
	}
	return logproto.SeriesIdentifier{
		Labels: series,
	}
}
