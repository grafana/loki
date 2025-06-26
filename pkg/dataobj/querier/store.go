package querier

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/tracing"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var tracer = otel.Tracer("pkg/dataobj/querier")

var (
	_ querier.Store = &Store{}

	noShard = logql.Shard{
		PowerOfTwo: &index.ShardAnnotation{
			Shard: uint32(0),
			Of:    uint32(1),
		},
	}

	shardedObjectsPool = sync.Pool{
		New: func() any {
			return &shardedObject{
				streams:    make(map[int64]streams.Stream),
				streamsIDs: make([]int64, 0, 1024),
				logReaders: make([]*logs.RowReader, 0, 16),
			}
		},
	}
	logReaderPool = sync.Pool{
		New: func() any {
			return &logs.RowReader{}
		},
	}
	streamReaderPool = sync.Pool{
		New: func() any {
			return &streams.RowReader{}
		},
	}
)

type Config struct {
	Enabled     bool                  `yaml:"enabled" doc:"description=Enable the dataobj querier."`
	From        storageconfig.DayTime `yaml:"from" doc:"description=The date of the first day of when the dataobj querier should start querying from. In YYYY-MM-DD format, for example: 2018-04-15."`
	ShardFactor int                   `yaml:"shard_factor" doc:"description=The number of shards to use for the dataobj querier."`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "dataobj-querier-enabled", false, "Enable the dataobj querier.")
	f.Var(&c.From, "dataobj-querier-from", "The start time to query from.")
	f.IntVar(&c.ShardFactor, "dataobj-querier-shard-factor", 32, "The number of shards to use for the dataobj querier.")
}

func (c *Config) Validate() error {
	if c.Enabled && c.From.ModelTime().Time().IsZero() {
		return fmt.Errorf("from is required when dataobj querier is enabled")
	}
	return nil
}

func (c *Config) PeriodConfig() config.PeriodConfig {
	return config.PeriodConfig{
		From:      c.From,
		RowShards: uint32(c.ShardFactor),
		Schema:    "v13",
	}
}

// Store implements querier.Store for querying data objects.
type Store struct {
	bucket    objstore.Bucket
	logger    log.Logger
	metastore metastore.Metastore
}

// NewStore creates a new Store.
func NewStore(bucket objstore.Bucket, logger log.Logger, metastore metastore.Metastore) *Store {
	return &Store{
		bucket:    bucket,
		logger:    logger,
		metastore: metastore,
	}
}

func (s *Store) String() string {
	return "dataobj"
}

// SelectLogs implements querier.Store
func (s *Store) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	logger := util_log.WithContext(ctx, s.logger)

	objects, err := s.objectsForTimeRange(ctx, req.Start, req.End, logger)
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return iter.NoopEntryIterator, nil
	}

	shard, err := parseShards(req.Shards)
	if err != nil {
		return nil, err
	}

	return selectLogs(ctx, objects, shard, req, logger)
}

// SelectSamples implements querier.Store
func (s *Store) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	logger := util_log.WithContext(ctx, s.logger)

	objects, err := s.objectsForTimeRange(ctx, req.Start, req.End, logger)
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return iter.NoopSampleIterator, nil
	}

	shard, err := parseShards(req.Shards)
	if err != nil {
		return nil, err
	}
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}

	return selectSamples(ctx, objects, shard, expr, req.Start, req.End, logger)
}

// Stats implements querier.Store
func (s *Store) Stats(_ context.Context, _ string, _ model.Time, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	// TODO: Implement
	return &stats.Stats{}, nil
}

// Volume implements querier.Store
func (s *Store) Volume(_ context.Context, _ string, _ model.Time, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	// TODO: Implement
	return &logproto.VolumeResponse{}, nil
}

// GetShards implements querier.Store
func (s *Store) GetShards(_ context.Context, _ string, _ model.Time, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	// TODO: Implement
	return &logproto.ShardsResponse{}, nil
}

type object struct {
	*dataobj.Object
	path string
}

// objectsForTimeRange returns data objects for the given time range.
func (s *Store) objectsForTimeRange(ctx context.Context, from, through time.Time, logger log.Logger) ([]object, error) {
	ctx, span := tracer.Start(ctx, "objectsForTimeRange")
	defer span.End()

	span.SetAttributes(
		attribute.String("from", from.String()),
		attribute.String("through", through.String()),
	)

	files, err := s.metastore.DataObjects(ctx, from, through)
	if err != nil {
		return nil, err
	}

	level.Debug(logger).Log(
		"msg", "found data objects for time range",
		"count", len(files),
		"from", from,
		"through", through,
	)
	span.AddEvent("found data objects for time range", trace.WithAttributes(
		attribute.Int("count", len(files)),
		attribute.StringSlice("files", files)),
	)

	objects := make([]object, 0, len(files))
	for _, path := range files {
		obj, err := dataobj.FromBucket(ctx, s.bucket, path)
		if err != nil {
			return nil, fmt.Errorf("getting object from bucket: %w", err)
		}
		objects = append(objects, object{Object: obj, path: path})
	}
	return objects, nil
}

func selectLogs(ctx context.Context, objects []object, shard logql.Shard, req logql.SelectLogParams, logger log.Logger) (iter.EntryIterator, error) {
	selector, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	shardedObjects, err := shardObjects(ctx, objects, shard, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, obj := range shardedObjects {
			obj.reset()
			shardedObjectsPool.Put(obj)
		}
	}()
	streamsPredicate := streamPredicate(selector.Matchers(), req.Start, req.End)
	var logsPredicates []logs.RowPredicate
	logsPredicates = append(logsPredicates, logs.TimeRangeRowPredicate{
		StartTime:    req.Start,
		EndTime:      req.End,
		IncludeStart: true,
		IncludeEnd:   false,
	})

	p, expr := buildLogsPredicateFromPipeline(selector)
	if p != nil {
		logsPredicates = append(logsPredicates, p)
	}
	req.Plan.AST = expr

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.EntryIterator, len(shardedObjects))

	for i, obj := range shardedObjects {
		g.Go(func() error {
			ctx, span := tracer.Start(ctx, "object selectLogs", trace.WithAttributes(
				attribute.String("object", obj.object.path),
				attribute.Int("sections", len(obj.logReaders)),
			))
			defer span.End()

			iterator, err := obj.selectLogs(ctx, streamsPredicate, logsPredicates, req)
			if err != nil {
				return err
			}
			iterators[i] = iterator
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return iter.NewSortEntryIterator(iterators, req.Direction), nil
}

func selectSamples(ctx context.Context, objects []object, shard logql.Shard, expr syntax.SampleExpr, start, end time.Time, logger log.Logger) (iter.SampleIterator, error) {
	shardedObjects, err := shardObjects(ctx, objects, shard, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, obj := range shardedObjects {
			obj.reset()
			shardedObjectsPool.Put(obj)
		}
	}()
	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}

	streamsPredicate := streamPredicate(selector.Matchers(), start, end)
	// TODO: support more predicates and combine with log.Pipeline.
	var logsPredicates []logs.RowPredicate
	logsPredicates = append(logsPredicates, logs.TimeRangeRowPredicate{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   false,
	})

	var predicateFromExpr logs.RowPredicate
	predicateFromExpr, expr = buildLogsPredicateFromSampleExpr(expr)
	if predicateFromExpr != nil {
		logsPredicates = append(logsPredicates, predicateFromExpr)
	}

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.SampleIterator, len(shardedObjects))

	for i, obj := range shardedObjects {
		g.Go(func() error {
			ctx, span := tracer.Start(ctx, "object selectSamples", trace.WithAttributes(
				attribute.String("object", obj.object.path),
				attribute.Int("sections", len(obj.logReaders)),
			))

			defer span.End()

			iterator, err := obj.selectSamples(ctx, streamsPredicate, logsPredicates, expr)
			if err != nil {
				return err
			}
			iterators[i] = iterator
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return iter.NewSortSampleIterator(iterators), nil
}

type shardedObject struct {
	object       object
	streamReader *streams.RowReader
	logReaders   []*logs.RowReader

	streamsIDs []int64
	streams    map[int64]streams.Stream
}

// shardSections returns a list of section indices to read per metadata based on the sharding configuration.
// The returned slice has the same length as the input metadatas, and each element contains the list of section indices
// that should be read for that metadata.
func shardSections(metadatas []sectionsStats, shard logql.Shard) [][]int {
	// Count total sections before sharding
	var totalSections int
	for _, metadata := range metadatas {
		totalSections += metadata.LogsSections
		if metadata.StreamsSections > 1 {
			// We don't support multiple streams sections, but we still need to return a slice
			// with the same length as the input metadatas.
			return make([][]int, len(metadatas))
		}
	}

	// sectionIndex tracks the global section number across all objects to ensure consistent sharding
	var sectionIndex uint64
	result := make([][]int, len(metadatas))

	for i, metadata := range metadatas {
		sections := make([]int, 0, metadata.LogsSections)
		for j := 0; j < metadata.LogsSections; j++ {
			if shard.PowerOfTwo != nil && shard.PowerOfTwo.Of > 1 {
				if sectionIndex%uint64(shard.PowerOfTwo.Of) != uint64(shard.PowerOfTwo.Shard) {
					sectionIndex++
					continue
				}
			}
			sections = append(sections, j)
			sectionIndex++
		}
		result[i] = sections
	}

	return result
}

func shardObjects(
	ctx context.Context,
	objects []object,
	shard logql.Shard,
	logger log.Logger,
) ([]*shardedObject, error) {
	ctx, span := tracer.Start(ctx, "shardObjects")
	defer span.End()

	metadatas, err := fetchSectionsStats(ctx, objects)
	if err != nil {
		return nil, err
	}

	// Get the sections to read per metadata
	sectionsPerMetadata := shardSections(metadatas, shard)

	// Count total sections that will be read
	var totalSections int
	var objectSections []int
	for i, sections := range sectionsPerMetadata {
		totalSections += len(sections)
		objectSections = append(objectSections, metadatas[i].LogsSections)
	}

	shardedReaders := make([]*shardedObject, 0, len(objects))

	for i, sections := range sectionsPerMetadata {
		if len(sections) == 0 {
			continue
		}

		reader := shardedObjectsPool.Get().(*shardedObject)
		reader.streamReader = streamReaderPool.Get().(*streams.RowReader)
		reader.object = objects[i]

		sec, err := findStreamsSection(ctx, objects[i].Object)
		if err != nil {
			return nil, fmt.Errorf("finding streams section: %w", err)
		}
		reader.streamReader.Reset(sec)

		for _, section := range sections {
			sec, err := findLogsSection(ctx, objects[i].Object, section)
			if err != nil {
				return nil, fmt.Errorf("finding logs section: %w", err)
			}

			logReader := logReaderPool.Get().(*logs.RowReader)
			logReader.Reset(sec)
			reader.logReaders = append(reader.logReaders, logReader)
		}
		shardedReaders = append(shardedReaders, reader)
	}
	var sectionsString strings.Builder
	for _, sections := range sectionsPerMetadata {
		sectionsString.WriteString(fmt.Sprintf("%v ", sections))
	}

	logParams := []interface{}{
		"msg", "sharding sections",
		"sharded_factor", shard.String(),
		"total_objects", len(objects),
		"total_sections", totalSections,
		"object_sections", fmt.Sprintf("%v", objectSections),
		"sharded_total_objects", len(shardedReaders),
		"sharded_sections", sectionsString.String(),
	}

	level.Debug(logger).Log(logParams...)
	sp := trace.SpanFromContext(ctx)
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(logParams)...)

	return shardedReaders, nil
}

func findLogsSection(ctx context.Context, obj *dataobj.Object, index int) (*logs.Section, error) {
	var count int

	for _, section := range obj.Sections() {
		if !logs.CheckSection(section) {
			continue
		}
		if count == index {
			return logs.Open(ctx, section)
		}
		count++
	}

	return nil, fmt.Errorf("object does not have logs section %d (max %d)", index, count)
}

func findStreamsSection(ctx context.Context, obj *dataobj.Object) (*streams.Section, error) {
	for _, section := range obj.Sections() {
		if !streams.CheckSection(section) {
			continue
		}
		return streams.Open(ctx, section)
	}

	return nil, fmt.Errorf("object has no streams sections")
}

func (s *shardedObject) reset() {
	_ = s.streamReader.Close()
	streamReaderPool.Put(s.streamReader)
	for i, reader := range s.logReaders {
		_ = reader.Close()
		logReaderPool.Put(reader)
		s.logReaders[i] = nil
	}
	s.streamReader = nil
	s.logReaders = s.logReaders[:0]
	s.streamsIDs = s.streamsIDs[:0]
	s.object = object{}
	clear(s.streams)
}

func (s *shardedObject) selectLogs(ctx context.Context, streamsPredicate streams.RowPredicate, logsPredicates []logs.RowPredicate, req logql.SelectLogParams) (iter.EntryIterator, error) {
	if err := s.setPredicate(streamsPredicate, logsPredicates); err != nil {
		return nil, err
	}

	if err := s.matchStreams(ctx); err != nil {
		return nil, err
	}
	iterators := make([]iter.EntryIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			sp := trace.SpanFromContext(ctx)
			sp.AddEvent("starting selectLogs in section", trace.WithAttributes(
				attribute.Int("index", i),
			))
			defer func() {
				sp.AddEvent("selectLogs section done", trace.WithAttributes(
					attribute.Int("index", i),
				))
			}()

			iter, err := newEntryIterator(ctx, s.streams, reader, req)
			if err != nil {
				return err
			}

			iterators[i] = iter
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return iter.NewSortEntryIterator(iterators, req.Direction), nil
}

func (s *shardedObject) selectSamples(ctx context.Context, streamsPredicate streams.RowPredicate, logsPredicates []logs.RowPredicate, expr syntax.SampleExpr) (iter.SampleIterator, error) {
	if err := s.setPredicate(streamsPredicate, logsPredicates); err != nil {
		return nil, err
	}

	if err := s.matchStreams(ctx); err != nil {
		return nil, err
	}

	iterators := make([]iter.SampleIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			sp := trace.SpanFromContext(ctx)
			sp.AddEvent("starting selectSamples in section", trace.WithAttributes(
				attribute.Int("index", i),
			))
			defer func() {
				sp.AddEvent("selectSamples section done", trace.WithAttributes(
					attribute.Int("index", i),
				))
			}()

			// extractors is not thread safe, so we need to create a new one for each object
			extractors, err := expr.Extractors()
			if err != nil {
				return err
			}
			iter, err := newSampleIterator(ctx, s.streams, extractors, reader)
			if err != nil {
				return err
			}
			iterators[i] = iter
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return iter.NewSortSampleIterator(iterators), nil
}

func (s *shardedObject) setPredicate(streamsPredicate streams.RowPredicate, logsPredicates []logs.RowPredicate) error {
	if err := s.streamReader.SetPredicate(streamsPredicate); err != nil {
		return err
	}
	for _, reader := range s.logReaders {
		if err := reader.SetPredicates(logsPredicates); err != nil {
			return err
		}
	}
	return nil
}

func (s *shardedObject) matchStreams(ctx context.Context) error {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("starting matchStreams")
	defer sp.AddEvent("matchStreams done")

	streamsPtr := streamsPool.Get().(*[]streams.Stream)
	defer streamsPool.Put(streamsPtr)
	streams := *streamsPtr

	for {
		n, err := s.streamReader.Read(ctx, streams)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 && err == io.EOF {
			break
		}

		for _, stream := range streams[:n] {
			s.streams[stream.ID] = stream
			s.streamsIDs = append(s.streamsIDs, stream.ID)
		}
	}
	// setup log readers to filter streams
	for _, reader := range s.logReaders {
		if err := reader.MatchStreams(slices.Values(s.streamsIDs)); err != nil {
			return err
		}
	}
	return nil
}

// fetchSectionsStats retrieves section count of objects.
func fetchSectionsStats(ctx context.Context, objects []object) ([]sectionsStats, error) {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("fetching metadata", trace.WithAttributes(
		attribute.Int("objects", len(objects)),
	))
	defer sp.AddEvent("fetched metadata")

	res := make([]sectionsStats, 0, len(objects))

	for _, obj := range objects {
		var stats sectionsStats

		for _, section := range obj.Sections() {
			switch {
			case streams.CheckSection(section):
				stats.StreamsSections++
			case logs.CheckSection(section):
				stats.LogsSections++
			}
		}

		res = append(res, stats)
	}

	return res, nil
}

type sectionsStats struct {
	StreamsSections int
	LogsSections    int
}

// streamPredicate creates a dataobj.StreamsPredicate from a list of matchers and a time range
func streamPredicate(matchers []*labels.Matcher, start, end time.Time) streams.RowPredicate {
	var predicate streams.RowPredicate = streams.TimeRangeRowPredicate{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   true,
	}

	// If there are any matchers, combine them with an AND predicate
	if len(matchers) > 0 {
		predicate = streams.AndRowPredicate{
			Left:  predicate,
			Right: matchersToPredicate(matchers),
		}
	}
	return predicate
}

// matchersToPredicate converts a list of matchers to a dataobj.StreamsPredicate
func matchersToPredicate(matchers []*labels.Matcher) streams.RowPredicate {
	var left streams.RowPredicate
	for _, matcher := range matchers {
		var right streams.RowPredicate
		switch matcher.Type {
		case labels.MatchEqual:
			right = streams.LabelMatcherRowPredicate{Name: matcher.Name, Value: matcher.Value}
		default:
			right = streams.LabelFilterRowPredicate{Name: matcher.Name, Keep: func(_, value string) bool {
				return matcher.Matches(value)
			}}
		}
		if left == nil {
			left = right
		} else {
			left = streams.AndRowPredicate{
				Left:  left,
				Right: right,
			}
		}
	}
	return left
}

func parseShards(shards []string) (logql.Shard, error) {
	if len(shards) == 0 {
		return noShard, nil
	}
	parsed, _, err := logql.ParseShards(shards)
	if err != nil {
		return noShard, err
	}
	if len(parsed) == 0 {
		return noShard, nil
	}
	if parsed[0].Variant() != logql.PowerOfTwoVersion {
		return noShard, fmt.Errorf("unsupported shard variant: %s", parsed[0].Variant())
	}
	return parsed[0], nil
}

func buildLogsPredicateFromSampleExpr(expr syntax.SampleExpr) (logs.RowPredicate, syntax.SampleExpr) {
	var (
		predicate logs.RowPredicate
		skip      bool
	)
	expr.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.BinOpExpr:
			// we might not encounter BinOpExpr at this point since the lhs and rhs are evaluated separately?
			skip = true
		case *syntax.RangeAggregationExpr:
			if !skip {
				predicate, e.Left.Left = buildLogsPredicateFromPipeline(e.Left.Left)
			}
		}
		return true
	})

	return predicate, expr
}

func buildLogsPredicateFromPipeline(expr syntax.LogSelectorExpr) (logs.RowPredicate, syntax.LogSelectorExpr) {
	// Check if expr is a PipelineExpr, other implementations have no stages
	pipelineExpr, ok := expr.(*syntax.PipelineExpr)
	if !ok {
		return nil, expr
	}

	var (
		predicate       logs.RowPredicate
		remainingStages = make([]syntax.StageExpr, 0, len(pipelineExpr.MultiStages))
		appendPredicate = func(p logs.RowPredicate) {
			if predicate == nil {
				predicate = p
			} else {
				predicate = logs.AndRowPredicate{
					Left:  predicate,
					Right: p,
				}
			}
		}
	)

Outer:
	for i, stage := range pipelineExpr.MultiStages {
		switch s := stage.(type) {
		case *syntax.LineFmtExpr:
			// modifies the log line, break early as we cannot apply any more predicates
			remainingStages = append(remainingStages, pipelineExpr.MultiStages[i:]...)
			break Outer

		case *syntax.LineFilterExpr:
			// Convert the line filter to a predicate
			f, err := s.Filter()
			if err != nil {
				remainingStages = append(remainingStages, s)
				continue
			}

			// Create a line filter predicate
			appendPredicate(logs.LogMessageFilterRowPredicate{
				Keep: func(line []byte) bool {
					return f.Filter(line)
				},
			})

		default:
			remainingStages = append(remainingStages, s)
		}
	}

	if len(remainingStages) == 0 {
		return predicate, pipelineExpr.Left // return MatchersExpr
	}
	pipelineExpr.MultiStages = remainingStages

	return predicate, pipelineExpr
}
