package bench

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

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
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var tracer = otel.Tracer("pkg/dataobj/querier")

var (
	_ querier.Store = &ParquetQuerier{}

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
type ParquetQuerier struct {
	bucket objstore.Bucket
	logger log.Logger
}

// NewStore creates a new Store.
func NewParquetQuerier(bucket objstore.Bucket, logger log.Logger) *ParquetQuerier {
	return &ParquetQuerier{
		bucket: bucket,
		logger: logger,
	}
}

func (s *ParquetQuerier) String() string {
	return "querier_parquet"
}

// SelectLogs implements querier.Store
func (s *ParquetQuerier) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
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
func (s *ParquetQuerier) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
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
func (s *ParquetQuerier) Stats(_ context.Context, _ string, _ model.Time, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	// TODO: Implement
	return &stats.Stats{}, nil
}

// Volume implements querier.Store
func (s *ParquetQuerier) Volume(_ context.Context, _ string, _ model.Time, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	// TODO: Implement
	return &logproto.VolumeResponse{}, nil
}

// GetShards implements querier.Store
func (s *ParquetQuerier) GetShards(_ context.Context, _ string, _ model.Time, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	// TODO: Implement
	return &logproto.ShardsResponse{}, nil
}

type object struct {
	*parquet.File
	path string
}

var _ io.ReaderAt = &bucketReaderAt{}

type bucketReaderAt struct {
	bucket objstore.Bucket
	path   string
}

func (b *bucketReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	reader, err := b.bucket.GetRange(context.Background(), b.path, off, int64(len(p)))
	if err != nil {
		return 0, err
	}
	return reader.Read(p)
}

// objectsForTimeRange returns data objects for the given time range.
func (s *ParquetQuerier) objectsForTimeRange(ctx context.Context, from, through time.Time, logger log.Logger) ([]object, error) {
	ctx, span := tracer.Start(ctx, "objectsForTimeRange")
	defer span.End()

	span.SetAttributes(
		attribute.String("from", from.String()),
		attribute.String("through", through.String()),
	)

	files := make([]object, 0)
	s.bucket.Iter(ctx, "tenant-test-tenant", func(name string) error {
		if strings.Contains(name, "parquet") {
			attrs, err := s.bucket.Attributes(ctx, name)
			if err != nil {
				return err
			}
			reader := &bucketReaderAt{
				bucket: s.bucket,
				path:   name,
			}
			parquet, err := parquet.OpenFile(reader, attrs.Size)
			if err != nil {
				return err
			}
			files = append(files, object{File: parquet})
		}
		return nil
	})

	level.Debug(logger).Log(
		"msg", "found data objects for time range",
		"count", len(files),
		"from", from,
		"through", through,
	)
	span.AddEvent("found parquet files for time range", trace.WithAttributes(
		attribute.Int("count", len(files)),
		//attribute.StringSlice("files", files),
	))

	return files, nil
}

func selectLogs(ctx context.Context, objects []object, shard logql.Shard, req logql.SelectLogParams, logger log.Logger) (iter.EntryIterator, error) {
	selector, err := req.LogSelector()
	if err != nil {
		return nil, err
	}

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
	iterators := make([]iter.EntryIterator, len(objects))

	for i, obj := range objects {
		g.Go(func() error {
			ctx, span := tracer.Start(ctx, "object selectLogs", trace.WithAttributes(
				attribute.String("object", obj.path),
			))
			defer span.End()

			iterator, err := obj.selectLogs(ctx, req)
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
	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}

	streamMatchers := map[string]string{}
	for _, matcher := range selector.Matchers() {
		streamMatchers[matcher.Name] = matcher.Value
	}

	_, expr = buildLogsPredicateFromSampleExpr(expr)

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.SampleIterator, len(objects))

	for i, obj := range objects {
		g.Go(func() error {
			ctx, span := tracer.Start(ctx, "object selectSamples", trace.WithAttributes(
				attribute.String("object", obj.path),
			))

			defer span.End()

			iterator, err := obj.selectSamples(ctx, expr, start, end)
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

func (s *object) selectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	iterators := make([]iter.EntryIterator, 1)
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		/* 		sp := trace.SpanFromContext(ctx)
		   		sp.AddEvent("starting selectLogs in section", trace.WithAttributes(
		   			attribute.Int("index", i),
		   		))
		   		defer func() {
		   			sp.AddEvent("selectLogs section done", trace.WithAttributes(
		   				attribute.Int("index", i),
		   			))
		   		}() */

		iter, err := newEntryIterator(ctx, s.File, req)
		if err != nil {
			return err
		}

		iterators[0] = iter
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return iter.NewSortEntryIterator(iterators, req.Direction), nil
}

func (s *object) selectSamples(ctx context.Context, expr syntax.SampleExpr, start, end time.Time) (iter.SampleIterator, error) {

	iterators := make([]iter.SampleIterator, 1)
	var err error
	iterators[0], err = newSampleIterator(ctx, expr, s.File, start, end)
	if err != nil {
		return nil, err
	}
	return iter.NewSortSampleIterator(iterators), nil
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
