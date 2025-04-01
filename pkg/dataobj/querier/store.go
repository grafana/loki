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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	logqllog "github.com/grafana/loki/v3/pkg/logql/log"
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

type PredicatePushdownConfig struct {
	EnableForLineFilters     bool `yaml:"enable_for_line_filters"`
	EnableForMetadataFilters bool `yaml:"enable_for_metadata_filters"`
}

func (c *PredicatePushdownConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&c.EnableForLineFilters, prefix+"predicate-pushdown.enable-for-line-filters", true, "Apply line filter predicates on dataobj reader.")
	f.BoolVar(&c.EnableForMetadataFilters, prefix+"predicate-pushdown.enable-for-metadata-filters", true, "Apply metadata filter predicates on dataobj reader.")
}

type Config struct {
	Enabled     bool                  `yaml:"enabled" doc:"description=Enable the dataobj querier."`
	From        storageconfig.DayTime `yaml:"from" doc:"description=The date of the first day of when the dataobj querier should start querying from. In YYYY-MM-DD format, for example: 2018-04-15."`
	ShardFactor int                   `yaml:"shard_factor" doc:"description=The number of shards to use for the dataobj querier."`

	// Experimental: Configuration for predicate pushdown optimizations.
	// Predicate pushdown helps reduce the number of rows we need to materialize
	// by filtering our pages and rows that do not match the applied predicates.
	// Not every line and label filter can be pushed down.
	PredicatePushdown PredicatePushdownConfig `yaml:"predicate_pushdown"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "dataobj-querier-enabled", false, "Enable the dataobj querier.")
	f.Var(&c.From, "dataobj-querier-from", "The start time to query from.")
	f.IntVar(&c.ShardFactor, "dataobj-querier-shard-factor", 32, "The number of shards to use for the dataobj querier.")
	c.PredicatePushdown.RegisterFlagsWithPrefix("dataobj-querier.", f)
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
	cfg       Config
	bucket    objstore.Bucket
	logger    log.Logger
	metastore metastore.Metastore
}

// NewStore creates a new Store.
func NewStore(cfg Config, bucket objstore.Bucket, logger log.Logger, metastore metastore.Metastore) *Store {
	return &Store{
		cfg:       cfg,
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

	return selectLogs(ctx, objects, shard, req, &s.cfg.PredicatePushdown, logger)
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

	return selectSamples(ctx, objects, shard, expr, req.Start, req.End, &s.cfg.PredicatePushdown, logger)
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

func selectLogs(ctx context.Context, objects []object, shard logql.Shard, req logql.SelectLogParams, predicatePushdownCfg *PredicatePushdownConfig, logger log.Logger) (iter.EntryIterator, error) {
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
	var logsPredicate dataobj.LogsPredicate = dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
		StartTime:    req.Start,
		EndTime:      req.End,
		IncludeStart: true,
		IncludeEnd:   false,
	}

	if predicatePushdownCfg.EnableForLineFilters {
		clonedExpr, err := syntax.Clone(selector)
		if err != nil {
			return nil, err
		}

		if p, expr := buildLogMessagePredicateFromPipeline(clonedExpr); p != nil {
			logsPredicate = dataobj.AndPredicate[dataobj.LogsPredicate]{
				Left:  logsPredicate,
				Right: p,
			}

			req.Plan.AST = expr
			level.Debug(logger).Log("msg", "line filter predicate pushdown", "orig_expr", selector.String(), "updated_expr", expr.String(), "log_message_filter_predicate", p.String())
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.EntryIterator, len(shardedObjects))

	for i, obj := range shardedObjects {
		g.Go(func() error {
			span, ctx := opentracing.StartSpanFromContext(ctx, "object selectLogs")
			defer span.Finish()
			span.SetTag("object", obj.object.path)
			span.SetTag("sections", len(obj.logReaders))

			iterator, err := obj.selectLogs(ctx, streamsPredicate, logsPredicate, req, predicatePushdownCfg, logger)
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

func selectSamples(ctx context.Context, objects []object, shard logql.Shard, expr syntax.SampleExpr, start, end time.Time, predicatePushdownCfg *PredicatePushdownConfig, logger log.Logger) (iter.SampleIterator, error) {
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
	var logsPredicate dataobj.LogsPredicate = dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   false,
	}

	if predicatePushdownCfg.EnableForLineFilters {
		cloned, err := syntax.Clone(expr)
		if err != nil {
			return nil, err
		}

		if p, updatedExpr := buildLogMessagePredicateFromSampleExpr(cloned); p != nil {
			logsPredicate = dataobj.AndPredicate[dataobj.LogsPredicate]{
				Left:  logsPredicate,
				Right: p,
			}

			level.Debug(logger).Log("msg", "line filter predicate pushdown", "orig_expr", expr.String(), "updated_expr", updatedExpr.String(), "log_message_filter_predicate", p.String())
			expr = updatedExpr
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.SampleIterator, len(shardedObjects))

	for i, obj := range shardedObjects {
		g.Go(func() error {
			span, ctx := opentracing.StartSpanFromContext(ctx, "object selectSamples")
			defer span.Finish()
			span.SetTag("object", obj.object.path)
			span.SetTag("sections", len(obj.logReaders))

			iterator, err := obj.selectSamples(ctx, streamsPredicate, logsPredicate, expr, predicatePushdownCfg, logger)
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

func (s *shardedObject) selectLogs(ctx context.Context, streamsPredicate dataobj.StreamsPredicate, logsPredicate dataobj.LogsPredicate, req logql.SelectLogParams, predicatePushdownCfg *PredicatePushdownConfig, logger log.Logger) (iter.EntryIterator, error) {
	if err := s.matchStreams(ctx, streamsPredicate); err != nil {
		return nil, err
	}

	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}

	applyLogsPredicate := func(r *dataobj.LogsReader) (syntax.LogSelectorExpr, error) {
		if !predicatePushdownCfg.EnableForMetadataFilters {
			return expr, r.SetPredicate(logsPredicate)
		}

		columns, err := r.Columns(ctx)
		if err != nil {
			level.Error(logger).Log("msg", "failed to read columns desc", "object", s.object.path, "err", err)
			// skip label filter pushdown if we can't read columns
			return expr, r.SetPredicate(logsPredicate)
		}

		metadataColumns := make(map[string]struct{}, len(columns))
		for _, column := range columns {
			if column.Type == logsmd.COLUMN_TYPE_METADATA {
				metadataColumns[column.Info.GetName()] = struct{}{}
			}
		}

		clonedExpr, err := syntax.Clone(expr)
		if err != nil {
			return nil, err
		}
		finalPredicate := logsPredicate

		// metadata filter pushdown has to be done at a section level since each section has different set of metadata columns
		metadataPredicate, updatedExpr := buildMetadataFilterPredicateFromPipeline(clonedExpr, metadataColumns)
		if metadataPredicate != nil {
			finalPredicate = dataobj.AndPredicate[dataobj.LogsPredicate]{
				Left:  finalPredicate,
				Right: metadataPredicate,
			}

			level.Debug(logger).Log("msg", "metadata filter predicate pushdown", "orig_expr", expr.String(), "updated_expr", updatedExpr.String(), "metadata_filter_predicate", metadataPredicate.String())
		}

		if err := r.SetPredicate(finalPredicate); err != nil {
			return nil, err
		}

		return updatedExpr, nil
	}

	iterators := make([]iter.EntryIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				sp.LogKV("msg", "starting selectLogs in section", "index", i)
				defer sp.LogKV("msg", "selectLogs section done", "index", i)
			}

			updatedExpr, err := applyLogsPredicate(reader)
			if err != nil {
				return err
			}

			iter, err := newEntryIterator(ctx, s.streams, reader, updatedExpr, req.Limit, req.Direction)
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

func (s *shardedObject) selectSamples(ctx context.Context, streamsPredicate dataobj.StreamsPredicate, logsPredicate dataobj.LogsPredicate, expr syntax.SampleExpr, predicatePushdownCfg *PredicatePushdownConfig, logger log.Logger) (iter.SampleIterator, error) {
	if err := s.matchStreams(ctx, streamsPredicate); err != nil {
		return nil, err
	}

	applyLogsPredicate := func(r *dataobj.LogsReader) (syntax.SampleExpr, error) {
		if !predicatePushdownCfg.EnableForMetadataFilters {
			return expr, r.SetPredicate(logsPredicate)
		}

		columns, err := r.Columns(ctx)
		if err != nil {
			level.Error(logger).Log("msg", "failed to read columns desc", "object", s.object.path, "err", err)
			// skip label filter pushdown if we can't read columns
			return expr, r.SetPredicate(logsPredicate)
		}

		metadataColumns := make(map[string]struct{}, len(columns))
		for _, column := range columns {
			if column.Type == logsmd.COLUMN_TYPE_METADATA {
				metadataColumns[column.Info.GetName()] = struct{}{}
			}
		}

		finalPredicate := logsPredicate
		clonedExpr, err := syntax.Clone(expr)
		if err != nil {
			return nil, err
		}

		// metadata filter pushdown has to be done at a section level since each section has different set of metadata columns
		metadataPredicate, updatedExpr := buildMetadataFilterPredicateFromSampleExpr(clonedExpr, metadataColumns)
		if metadataPredicate != nil {
			finalPredicate = dataobj.AndPredicate[dataobj.LogsPredicate]{
				Left:  finalPredicate,
				Right: metadataPredicate,
			}

			level.Debug(logger).Log("msg", "metadata filter predicate pushdown", "orig_expr", expr.String(), "updated_expr", updatedExpr.String(), "metadata_filter_predicate", metadataPredicate.String())
		}

		if err := r.SetPredicate(finalPredicate); err != nil {
			return nil, err
		}

		return updatedExpr, nil
	}

	iterators := make([]iter.SampleIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				sp.LogKV("msg", "starting selectSamples in section", "index", i)
				defer sp.LogKV("msg", "selectSamples section done", "index", i)
			}

			expr, err := applyLogsPredicate(reader)
			if err != nil {
				return err
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

func (s *shardedObject) matchStreams(ctx context.Context, streamsPredicate dataobj.StreamsPredicate) error {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("msg", "starting matchStreams")
		defer sp.LogKV("msg", "matchStreams done")
	}

	if err := s.streamReader.SetPredicate(streamsPredicate); err != nil {
		return err
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

func buildLogMessagePredicateFromSampleExpr(expr syntax.SampleExpr) (dataobj.LogsPredicate, syntax.SampleExpr) {
	var (
		predicate dataobj.LogsPredicate
		skip      bool
	)
	expr.Walk(func(e syntax.Expr) {
		switch e := e.(type) {
		case *syntax.BinOpExpr:
			// we might not encounter BinOpExpr at this point since the lhs and rhs are evaluated separately?
			skip = true
			return
		case *syntax.RangeAggregationExpr:
			if skip {
				return
			}

			predicate, e.Left.Left = buildLogMessagePredicateFromPipeline(e.Left.Left)
		}
	})

	return predicate, expr
}

func buildLogMessagePredicateFromPipeline(expr syntax.LogSelectorExpr) (dataobj.LogsPredicate, syntax.LogSelectorExpr) {
	// Check if expr is a PipelineExpr, other implementations have no stages
	pipelineExpr, ok := expr.(*syntax.PipelineExpr)
	if !ok {
		return nil, expr
	}

	var (
		predicate       dataobj.LogsPredicate
		remainingStages = make([]syntax.StageExpr, 0, len(pipelineExpr.MultiStages))
		appendPredicate = func(p dataobj.LogsPredicate) {
			if predicate == nil {
				predicate = p
			} else {
				predicate = dataobj.AndPredicate[dataobj.LogsPredicate]{
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
			appendPredicate(dataobj.LogMessageFilterPredicate{
				Keep: func(line []byte) bool {
					return f.Filter(line)
				},
				Desc: s.String(),
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

func buildMetadataFilterPredicateFromSampleExpr(expr syntax.SampleExpr, metadataColumns map[string]struct{}) (dataobj.LogsPredicate, syntax.SampleExpr) {
	var (
		predicate dataobj.LogsPredicate
		skip      bool
	)
	expr.Walk(func(e syntax.Expr) {
		switch e := e.(type) {
		case *syntax.BinOpExpr:
			// we might not encounter BinOpExpr at this point since the lhs and rhs are evaluated separately?
			skip = true
			return
		case *syntax.RangeAggregationExpr:
			if skip {
				return
			}

			predicate, e.Left.Left = buildMetadataFilterPredicateFromPipeline(e.Left.Left, metadataColumns)
		}
	})

	return predicate, expr
}

func buildMetadataFilterPredicateFromPipeline(expr syntax.LogSelectorExpr, metadataColumns map[string]struct{}) (dataobj.LogsPredicate, syntax.LogSelectorExpr) {
	pipelineExpr, ok := expr.(*syntax.PipelineExpr)
	if !ok {
		return nil, expr
	}

	var predicate dataobj.LogsPredicate
	appendPredicate := func(p dataobj.LogsPredicate) {
		if predicate == nil {
			predicate = p
		} else {
			predicate = dataobj.AndPredicate[dataobj.LogsPredicate]{Left: predicate, Right: p}
		}
	}

	remainingStages := make([]syntax.StageExpr, 0, len(pipelineExpr.MultiStages))

Outer:
	for i, stage := range pipelineExpr.MultiStages {
		switch stage := stage.(type) {
		case *syntax.LabelFmtExpr, *syntax.LineParserExpr, *syntax.LogfmtParserExpr,
			*syntax.LogfmtExpressionParserExpr, *syntax.JSONExpressionParserExpr,
			*syntax.KeepLabelsExpr, *syntax.DropLabelsExpr:
			// These stages modify the label set. Labels filter appearing after them cannot be used for row filtering.
			// TODO(ashwanth): For expression parsers, we know exactly which labels are going to be extracted.
			// This information can be used to push down filters that operate on labels that are not modified.
			// Similarly logic applies to Keep and Drop stages.
			remainingStages = append(remainingStages, pipelineExpr.MultiStages[i:]...)
			break Outer
		case *syntax.LabelFilterExpr:
			p, expr := processLabelFilter(stage.LabelFilterer, metadataColumns)
			if p != nil {
				appendPredicate(p)
			}

			if expr != nil {
				remainingStages = append(remainingStages, &syntax.LabelFilterExpr{LabelFilterer: expr})
			}
		default:
			remainingStages = append(remainingStages, stage)
		}
	}

	if len(remainingStages) == 0 {
		return predicate, pipelineExpr.Left
	}

	pipelineExpr.MultiStages = remainingStages
	return predicate, pipelineExpr
}

// processLabelFilter converts a label filter expression to a [dataobj.LogsPredicate] if possible.
// If the expr cannot be fully converted to a predicate, the reduced expression is returned.
func processLabelFilter(expr logqllog.LabelFilterer, metadataColumns map[string]struct{}) (dataobj.LogsPredicate, logqllog.LabelFilterer) {
	var predicate dataobj.LogsPredicate
	switch e := expr.(type) {
	case *logqllog.StringLabelFilter:
		if _, ok := metadataColumns[e.Name]; !ok {
			return nil, expr
		}

		if e.Matcher.Type == labels.MatchEqual {
			predicate = dataobj.MetadataMatcherPredicate{
				Key:   e.Name,
				Value: e.Value,
			}
		} else if e.Matcher.Type == labels.MatchNotEqual {
			predicate = dataobj.NotPredicate[dataobj.LogsPredicate]{
				Inner: dataobj.MetadataMatcherPredicate{
					Key:   e.Name,
					Value: e.Value,
				},
			}
		} else {
			predicate = dataobj.MetadataFilterPredicate{
				Key:  e.Name,
				Desc: e.String(),
				Keep: func(_, value string) bool {
					return e.Matcher.Matches(value)
				},
			}
		}

		// expression is fully consumed
		return predicate, nil
	case *logqllog.LineFilterLabelFilter: // optimized filters
		if _, ok := metadataColumns[e.Name]; !ok {
			return nil, expr
		}

		if e.Matcher.Type == labels.MatchEqual {
			predicate = dataobj.MetadataMatcherPredicate{
				Key:   e.Name,
				Value: e.Value,
			}
		} else if e.Matcher.Type == labels.MatchNotEqual {
			predicate = dataobj.NotPredicate[dataobj.LogsPredicate]{
				Inner: dataobj.MetadataMatcherPredicate{
					Key:   e.Name,
					Value: e.Value,
				},
			}
		} else {
			predicate = dataobj.MetadataFilterPredicate{
				Key:  e.Name,
				Desc: e.String(),
				Keep: func(_, value string) bool {
					return e.Filter.Filter([]byte(value))
				},
			}
		}

		// expression is fully consumed
		return predicate, nil
	case *logqllog.BytesLabelFilter, *logqllog.DurationLabelFilter, *logqllog.NumericLabelFilter:
		// TODO(ashwanth): we need to support propagating parse errors to support these
	case *logqllog.BinaryLabelFilter:
		// AND vs OR predicate push-down strategy:
		// 1. AND operations: Either side can be pushed down independently, we can break down the filter
		//    across Predicate and PipelineExpr since sequential pruning is the correct behavior for AND filters.
		//    For example, filters on metadata columns can be pushed down as a Predicate while keeping the rest in the PipelineExpr.
		//
		// 2. OR operations: Both sides must be evaluated together to determine inclusion.
		//    We can only push down an OR if both operands can be fully pushed.
		if !e.And {
			leftP, leftExpr := processLabelFilter(e.Left, metadataColumns)
			rightP, rightExpr := processLabelFilter(e.Right, metadataColumns)

			// Apply OR predicate only if both operands can be fully pushed down (predicates created and no remaining expressions)
			if leftP != nil && rightP != nil && leftExpr == nil && rightExpr == nil {
				return dataobj.OrPredicate[dataobj.LogsPredicate]{Left: leftP, Right: rightP}, nil
			}

			// Otherwise, keep original behavior
			return nil, expr
		}

		// For AND operations, process both sides and combine results
		leftP, leftExpr := processLabelFilter(e.Left, metadataColumns)
		rightP, rightExpr := processLabelFilter(e.Right, metadataColumns)

		return reducePredicates(leftP, rightP), reduceLabelFilters(leftExpr, rightExpr)
	default:
	}

	return predicate, expr
}

func reducePredicates(left, right dataobj.LogsPredicate) dataobj.LogsPredicate {
	if left == nil && right == nil {
		return nil
	} else if left == nil {
		return right
	} else if right == nil {
		return left
	}

	return dataobj.AndPredicate[dataobj.LogsPredicate]{Left: left, Right: right}
}

func reduceLabelFilters(left, right logqllog.LabelFilterer) logqllog.LabelFilterer {
	if left == nil && right == nil {
		return nil
	} else if left == nil {
		return right
	} else if right == nil {
		return left
	}

	return logqllog.NewAndLabelFilter(left, right)
}
