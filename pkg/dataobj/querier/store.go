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
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
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
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

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
				streams:    make(map[int64]dataobj.Stream),
				streamsIDs: make([]int64, 0, 1024),
				logReaders: make([]*dataobj.LogsReader, 0, 16),
			}
		},
	}
	logReaderPool = sync.Pool{
		New: func() any {
			return &dataobj.LogsReader{}
		},
	}
	streamReaderPool = sync.Pool{
		New: func() any {
			return &dataobj.StreamsReader{}
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
	span, ctx := opentracing.StartSpanFromContext(ctx, "objectsForTimeRange")
	defer span.Finish()

	span.SetTag("from", from)
	span.SetTag("through", through)

	files, err := s.metastore.DataObjects(ctx, from, through)
	if err != nil {
		return nil, err
	}

	logParams := []interface{}{
		"msg", "found data objects for time range",
		"count", len(files),
		"from", from,
		"through", through,
	}
	level.Debug(logger).Log(logParams...)

	span.LogKV(logParams...)
	span.LogKV("files", files)

	objects := make([]object, 0, len(files))
	for _, path := range files {
		objects = append(objects, object{
			Object: dataobj.FromBucket(s.bucket, path),
			path:   path,
		})
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
	// TODO: support more predicates and combine with log.Pipeline.
	logsPredicate := dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
		StartTime:    req.Start,
		EndTime:      req.End,
		IncludeStart: true,
		IncludeEnd:   false,
	}
	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.EntryIterator, len(shardedObjects))

	for i, obj := range shardedObjects {
		g.Go(func() error {
			span, ctx := opentracing.StartSpanFromContext(ctx, "object selectLogs")
			defer span.Finish()
			span.SetTag("object", obj.object.path)
			span.SetTag("sections", len(obj.logReaders))

			iterator, err := obj.selectLogs(ctx, streamsPredicate, logsPredicate, req)
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
	logsPredicate := dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   false,
	}

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.SampleIterator, len(shardedObjects))

	for i, obj := range shardedObjects {
		g.Go(func() error {
			span, ctx := opentracing.StartSpanFromContext(ctx, "object selectSamples")
			defer span.Finish()
			span.SetTag("object", obj.object.path)
			span.SetTag("sections", len(obj.logReaders))

			iterator, err := obj.selectSamples(ctx, streamsPredicate, logsPredicate, expr)
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
	streamReader *dataobj.StreamsReader
	logReaders   []*dataobj.LogsReader

	streamsIDs []int64
	streams    map[int64]dataobj.Stream
}

// shardSections returns a list of section indices to read per metadata based on the sharding configuration.
// The returned slice has the same length as the input metadatas, and each element contains the list of section indices
// that should be read for that metadata.
func shardSections(metadatas []dataobj.Metadata, shard logql.Shard) [][]int {
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
	span, ctx := opentracing.StartSpanFromContext(ctx, "shardObjects")
	defer span.Finish()

	metadatas, err := fetchMetadatas(ctx, objects)
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
		reader.streamReader = streamReaderPool.Get().(*dataobj.StreamsReader)
		reader.object = objects[i]
		reader.streamReader.Reset(objects[i].Object, 0)

		for _, section := range sections {
			logReader := logReaderPool.Get().(*dataobj.LogsReader)
			logReader.Reset(objects[i].Object, section)
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
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV(logParams...)
	}

	return shardedReaders, nil
}

func (s *shardedObject) reset() {
	streamReaderPool.Put(s.streamReader)
	for i, reader := range s.logReaders {
		logReaderPool.Put(reader)
		s.logReaders[i] = nil
	}
	s.streamReader = nil
	s.logReaders = s.logReaders[:0]
	s.streamsIDs = s.streamsIDs[:0]
	s.object = object{}
	clear(s.streams)
}

func (s *shardedObject) selectLogs(ctx context.Context, streamsPredicate dataobj.StreamsPredicate, logsPredicate dataobj.LogsPredicate, req logql.SelectLogParams) (iter.EntryIterator, error) {
	if err := s.setPredicate(streamsPredicate, logsPredicate); err != nil {
		return nil, err
	}

	if err := s.matchStreams(ctx); err != nil {
		return nil, err
	}
	iterators := make([]iter.EntryIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				sp.LogKV("msg", "starting selectLogs in section", "index", i)
				defer sp.LogKV("msg", "selectLogs section done", "index", i)
			}
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

func (s *shardedObject) selectSamples(ctx context.Context, streamsPredicate dataobj.StreamsPredicate, logsPredicate dataobj.LogsPredicate, expr syntax.SampleExpr) (iter.SampleIterator, error) {
	if err := s.setPredicate(streamsPredicate, logsPredicate); err != nil {
		return nil, err
	}

	if err := s.matchStreams(ctx); err != nil {
		return nil, err
	}

	iterators := make([]iter.SampleIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				sp.LogKV("msg", "starting selectSamples in section", "index", i)
				defer sp.LogKV("msg", "selectSamples section done", "index", i)
			}
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

func (s *shardedObject) setPredicate(streamsPredicate dataobj.StreamsPredicate, logsPredicate dataobj.LogsPredicate) error {
	if err := s.streamReader.SetPredicate(streamsPredicate); err != nil {
		return err
	}
	for _, reader := range s.logReaders {
		if err := reader.SetPredicate(logsPredicate); err != nil {
			return err
		}
	}
	return nil
}

func (s *shardedObject) matchStreams(ctx context.Context) error {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("msg", "starting matchStreams")
		defer sp.LogKV("msg", "matchStreams done")
	}
	streamsPtr := streamsPool.Get().(*[]dataobj.Stream)
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

// fetchMetadatas fetches metadata of objects in parallel
func fetchMetadatas(ctx context.Context, objects []object) ([]dataobj.Metadata, error) {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("msg", "fetching metadata", "objects", len(objects))
		defer sp.LogKV("msg", "fetched metadata")
	}
	g, ctx := errgroup.WithContext(ctx)
	metadatas := make([]dataobj.Metadata, len(objects))
	for i, obj := range objects {
		g.Go(func() error {
			var err error
			metadatas[i], err = obj.Object.Metadata(ctx)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return metadatas, nil
}

// streamPredicate creates a dataobj.StreamsPredicate from a list of matchers and a time range
func streamPredicate(matchers []*labels.Matcher, start, end time.Time) dataobj.StreamsPredicate {
	var predicate dataobj.StreamsPredicate = dataobj.TimeRangePredicate[dataobj.StreamsPredicate]{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   true,
	}

	// If there are any matchers, combine them with an AND predicate
	if len(matchers) > 0 {
		predicate = dataobj.AndPredicate[dataobj.StreamsPredicate]{
			Left:  predicate,
			Right: matchersToPredicate(matchers),
		}
	}
	return predicate
}

// matchersToPredicate converts a list of matchers to a dataobj.StreamsPredicate
func matchersToPredicate(matchers []*labels.Matcher) dataobj.StreamsPredicate {
	var left dataobj.StreamsPredicate
	for _, matcher := range matchers {
		var right dataobj.StreamsPredicate
		switch matcher.Type {
		case labels.MatchEqual:
			right = dataobj.LabelMatcherPredicate{Name: matcher.Name, Value: matcher.Value}
		default:
			right = dataobj.LabelFilterPredicate{Name: matcher.Name, Keep: func(_, value string) bool {
				return matcher.Matches(value)
			}}
		}
		if left == nil {
			left = right
		} else {
			left = dataobj.AndPredicate[dataobj.StreamsPredicate]{
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
