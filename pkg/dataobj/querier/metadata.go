package querier

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var streamsPool = sync.Pool{
	New: func() any {
		streams := make([]dataobj.Stream, 1024)
		return &streams
	},
}

// SelectSeries implements querier.Store
func (s *Store) SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
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

	err = processor.ProcessParallel(ctx, func(h uint64, stream dataobj.Stream) {
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

	return result, nil
}

// LabelNamesForMetricName implements querier.Store
func (s *Store) LabelNamesForMetricName(ctx context.Context, _ string, from, through model.Time, _ string, matchers ...*labels.Matcher) ([]string, error) {
	logger := util_log.WithContext(ctx, s.logger)
	start, end := from.Time(), through.Time()
	objects, err := s.objectsForTimeRange(ctx, start, end, logger)
	if err != nil {
		return nil, err
	}

	if len(objects) == 0 {
		return nil, nil
	}

	processor := newStreamProcessor(start, end, matchers, objects, noShard, logger)
	uniqueNames := sync.Map{}

	err = processor.ProcessParallel(ctx, func(_ uint64, stream dataobj.Stream) {
		for _, label := range stream.Labels {
			uniqueNames.Store(label.Name, nil)
		}
	})
	if err != nil {
		return nil, err
	}

	names := []string{}
	uniqueNames.Range(func(key, _ interface{}) bool {
		names = append(names, key.(string))
		return true
	})

	sort.Strings(names)

	return names, nil
}

// LabelValuesForMetricName implements querier.Store
func (s *Store) LabelValuesForMetricName(ctx context.Context, _ string, from, through model.Time, _ string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	logger := util_log.WithContext(ctx, s.logger)
	start, end := from.Time(), through.Time()

	requireLabel, err := labels.NewMatcher(labels.MatchNotEqual, labelName, "")
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate label matcher: %w", err)
	}

	matchers = append(matchers, requireLabel)

	objects, err := s.objectsForTimeRange(ctx, start, end, logger)
	if err != nil {
		return nil, err
	}

	if len(objects) == 0 {
		return nil, nil
	}

	processor := newStreamProcessor(start, end, matchers, objects, noShard, logger)
	uniqueValues := sync.Map{}

	err = processor.ProcessParallel(ctx, func(_ uint64, stream dataobj.Stream) {
		uniqueValues.Store(stream.Labels.Get(labelName), nil)
	})
	if err != nil {
		return nil, err
	}

	values := []string{}
	uniqueValues.Range(func(key, _ interface{}) bool {
		values = append(values, key.(string))
		return true
	})

	sort.Strings(values)

	return values, nil
}

// streamProcessor handles processing of unique series with custom collection logic
type streamProcessor struct {
	predicate  dataobj.StreamsPredicate
	seenSeries *sync.Map
	objects    []object
	shard      logql.Shard
	logger     log.Logger
}

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
func (sp *streamProcessor) ProcessParallel(ctx context.Context, onNewStream func(uint64, dataobj.Stream)) error {
	readers, err := shardStreamReaders(ctx, sp.objects, sp.shard)
	if err != nil {
		return err
	}
	defer func() {
		for _, reader := range readers {
			streamReaderPool.Put(reader)
		}
	}()

	start := time.Now()
	span := opentracing.SpanFromContext(ctx)
	if span != nil {
		span.LogKV("msg", "processing streams", "total_readers", len(readers))
	}
	level.Debug(sp.logger).Log("msg", "processing streams", "total_readers", len(readers))

	// set predicate on all readers
	for _, reader := range readers {
		if err := reader.SetPredicate(sp.predicate); err != nil {
			return err
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	var processedStreams atomic.Int64
	for _, reader := range readers {
		g.Go(func() error {
			span, ctx := opentracing.StartSpanFromContext(ctx, "streamProcessor.processSingleReader")
			defer span.Finish()
			n, err := sp.processSingleReader(ctx, reader, onNewStream)
			if err != nil {
				return err
			}
			processedStreams.Add(n)
			return nil
		})
	}
	err = g.Wait()
	if err != nil {
		return err
	}

	level.Debug(sp.logger).Log("msg", "finished processing streams",
		"total_readers", len(readers),
		"total_streams_processed", processedStreams.Load(),
		"duration", time.Since(start),
	)
	if span != nil {
		span.LogKV("msg", "streamProcessor.ProcessParallel done", "total_readers", len(readers), "total_streams_processed", processedStreams.Load(), "duration", time.Since(start))
	}

	return nil
}

func (sp *streamProcessor) processSingleReader(ctx context.Context, reader *dataobj.StreamsReader, onNewStream func(uint64, dataobj.Stream)) (int64, error) {
	var (
		streamsPtr = streamsPool.Get().(*[]dataobj.Stream)
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

// shardStreamReaders fetches metadata of objects in parallel and shards them into a list of StreamsReaders
func shardStreamReaders(ctx context.Context, objects []object, shard logql.Shard) ([]*dataobj.StreamsReader, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "shardStreamReaders")
	defer span.Finish()

	span.SetTag("objects", len(objects))

	metadatas, err := fetchMetadatas(ctx, objects)
	if err != nil {
		return nil, err
	}

	// sectionIndex tracks the global section number across all objects to ensure consistent sharding
	var sectionIndex uint64
	var readers []*dataobj.StreamsReader
	for i, metadata := range metadatas {
		if metadata.StreamsSections > 1 {
			return nil, fmt.Errorf("unsupported multiple streams sections count: %d", metadata.StreamsSections)
		}

		// For sharded queries (e.g., "1 of 2"), we only read sections that belong to our shard
		// The section is assigned to a shard based on its global index across all objects
		if shard.PowerOfTwo != nil && shard.PowerOfTwo.Of > 1 {
			if sectionIndex%uint64(shard.PowerOfTwo.Of) != uint64(shard.PowerOfTwo.Shard) {
				sectionIndex++
				continue
			}
		}
		reader := streamReaderPool.Get().(*dataobj.StreamsReader)
		reader.Reset(objects[i].Object, 0)
		readers = append(readers, reader)
		sectionIndex++
	}
	span.LogKV("msg", "shardStreamReaders done", "readers", len(readers))
	return readers, nil
}
