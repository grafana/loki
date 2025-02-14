package querier

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

var streamsPool = sync.Pool{
	New: func() any {
		streams := make([]dataobj.Stream, 1024)
		return &streams
	},
}

// SelectSeries implements querier.Store
func (s *Store) SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	objects, err := s.objectsForTimeRange(ctx, req.Start, req.End)
	if err != nil {
		return nil, err
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

	processor := newStreamProcessor(req.Start, req.End, matchers, objects, shard)

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
	start, end := from.Time(), through.Time()
	objects, err := s.objectsForTimeRange(ctx, start, end)
	if err != nil {
		return nil, err
	}

	processor := newStreamProcessor(start, end, matchers, objects, noShard)
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
	start, end := from.Time(), through.Time()

	requireLabel, err := labels.NewMatcher(labels.MatchNotEqual, labelName, "")
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate label matcher: %w", err)
	}

	matchers = append(matchers, requireLabel)

	objects, err := s.objectsForTimeRange(ctx, start, end)
	if err != nil {
		return nil, err
	}

	processor := newStreamProcessor(start, end, matchers, objects, noShard)
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
	objects    []*dataobj.Object
	shard      logql.Shard
}

// newStreamProcessor creates a new streamProcessor with the given parameters
func newStreamProcessor(start, end time.Time, matchers []*labels.Matcher, objects []*dataobj.Object, shard logql.Shard) *streamProcessor {
	return &streamProcessor{
		predicate:  streamPredicate(matchers, start, end),
		seenSeries: &sync.Map{},
		objects:    objects,
		shard:      shard,
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

	// set predicate on all readers
	for _, reader := range readers {
		if err := reader.SetPredicate(sp.predicate); err != nil {
			return err
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, reader := range readers {
		g.Go(func() error {
			return sp.processSingleReader(ctx, reader, onNewStream)
		})
	}
	return g.Wait()
}

func (sp *streamProcessor) processSingleReader(ctx context.Context, reader *dataobj.StreamsReader, onNewStream func(uint64, dataobj.Stream)) error {
	var (
		streamsPtr = streamsPool.Get().(*[]dataobj.Stream)
		streams    = *streamsPtr
		buf        = make([]byte, 0, 1024)
		h          uint64
	)

	defer streamsPool.Put(streamsPtr)

	for {
		n, err := reader.Read(ctx, streams)
		if err != nil && err != io.EOF {
			return err
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
		}
	}
	return nil
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
func shardStreamReaders(ctx context.Context, objects []*dataobj.Object, shard logql.Shard) ([]*dataobj.StreamsReader, error) {
	metadatas, err := fetchMetadatas(ctx, objects)
	if err != nil {
		return nil, err
	}
	// sectionIndex tracks the global section number across all objects to ensure consistent sharding
	var sectionIndex uint64
	var readers []*dataobj.StreamsReader
	for i, metadata := range metadatas {
		for j := 0; j < metadata.StreamsSections; j++ {
			// For sharded queries (e.g., "1 of 2"), we only read sections that belong to our shard
			// The section is assigned to a shard based on its global index across all objects
			if shard.PowerOfTwo != nil && shard.PowerOfTwo.Of > 1 {
				if sectionIndex%uint64(shard.PowerOfTwo.Of) != uint64(shard.PowerOfTwo.Shard) {
					sectionIndex++
					continue
				}
			}
			reader := streamReaderPool.Get().(*dataobj.StreamsReader)
			reader.Reset(objects[i], j)
			readers = append(readers, reader)
			sectionIndex++
		}
	}
	return readers, nil
}
