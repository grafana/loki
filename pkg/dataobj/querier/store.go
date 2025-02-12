package querier

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/grafana/dskit/tenant"
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
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

var _ querier.Store = &Store{}

type Config struct {
	Enabled bool                  `yaml:"enabled" doc:"description=Enable the dataobj querier."`
	From    storageconfig.DayTime `yaml:"from" doc:"description=The date of the first day of when the dataobj querier should start querying from. In YYYY-MM-DD format, for example: 2018-04-15."`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "dataobj-querier-enabled", false, "Enable the dataobj querier.")
	f.Var(&c.From, "dataobj-querier-from", "The start time to query from.")
}

func (c *Config) Validate() error {
	if c.Enabled && c.From.ModelTime().Time().IsZero() {
		return fmt.Errorf("from is required when dataobj querier is enabled")
	}
	return nil
}

type Store struct {
	bucket objstore.Bucket
}

func NewStore(bucket objstore.Bucket) *Store {
	return &Store{
		bucket: bucket,
	}
}

// SelectLogs implements querier.Store
func (s *Store) SelectLogs(_ context.Context, _ logql.SelectLogParams) (iter.EntryIterator, error) {
	// TODO: Implement
	return iter.NoopEntryIterator, nil
}

// SelectSamples implements querier.Store
func (s *Store) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	objects, err := s.objectsForTimeRange(ctx, req.Start, req.End)
	if err != nil {
		return nil, err
	}

	shard, err := parseShards(req.Shards)
	if err != nil {
		return nil, err
	}
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}

	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}

	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}

	return selectSamples(ctx, objects, shard, selector.Matchers(), extractor, req.Start, req.End)
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

func (s *Store) objectsForTimeRange(ctx context.Context, from, through time.Time) ([]*dataobj.Object, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	files, err := metastore.ListDataObjects(ctx, s.bucket, userID, from, through)
	if err != nil {
		return nil, err
	}

	objects := make([]*dataobj.Object, 0, len(files))
	for _, path := range files {
		objects = append(objects, dataobj.FromBucket(s.bucket, path))
	}
	return objects, nil
}

var noShard = logql.Shard{
	PowerOfTwo: &index.ShardAnnotation{
		Shard: uint32(1),
		Of:    uint32(1),
	},
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
	return parsed[0], nil
}

func selectSamples(ctx context.Context, objects []*dataobj.Object, shard logql.Shard, matchers []*labels.Matcher, extractor syntax.SampleExtractor, start, end time.Time) (iter.SampleIterator, error) {
	shardedObjects, err := shardObjects(ctx, objects, shard)
	if err != nil {
		return nil, err
	}

	streamsPredicate := streamPredicate(matchers, start, end)
	// TODO: support more predicates and combine with log.Pipeline.
	logsPredicate := dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   false,
	}

	g, ctx := errgroup.WithContext(ctx)
	iterators := make([]iter.SampleIterator, len(shardedObjects))

	for i, reader := range shardedObjects {
		g.Go(func() error {
			iterators[i], err = reader.selectSamples(ctx, streamsPredicate, logsPredicate, extractor)
			if err != nil {
				return err
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return iter.NewSortSampleIterator(iterators), nil
}

type shardedObject struct {
	streamReader *dataobj.StreamsReader
	logReaders   []*dataobj.LogsReader

	streamsIDs []int64
	streams    map[int64]dataobj.Stream
}

func shardObjects(
	ctx context.Context,
	objects []*dataobj.Object,
	shard logql.Shard,
) ([]*shardedObject, error) {
	// fetch all metadata of objects in parallel
	g, ctx := errgroup.WithContext(ctx)
	metadatas := make([]dataobj.Metadata, len(objects))
	for i, obj := range objects {
		g.Go(func() error {
			var err error
			metadatas[i], err = obj.Metadata(ctx)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// sectionIndex tracks the global section number across all objects to ensure consistent sharding
	var sectionIndex uint64
	shardedReaders := make([]*shardedObject, 0, len(objects))

	for i, metadata := range metadatas {
		var reader shardedObject

		for j := 0; j < metadata.LogsSections; j++ {
			if shard.PowerOfTwo != nil && shard.PowerOfTwo.Of > 1 {
				if sectionIndex%uint64(shard.PowerOfTwo.Of) != uint64(shard.PowerOfTwo.Shard) {
					sectionIndex++
					continue
				}
			}

			if reader.streamReader == nil {
				reader.streamReader = dataobj.NewStreamsReader(objects[i], j)
			}
			reader.logReaders = append(reader.logReaders, dataobj.NewLogsReader(objects[i], j))
			sectionIndex++
		}

		if reader.streamReader != nil {
			shardedReaders = append(shardedReaders, &reader)
		}
	}

	return shardedReaders, nil
}

func (s *shardedObject) selectSamples(ctx context.Context, streamsPredicate dataobj.StreamsPredicate, logsPredicate dataobj.LogsPredicate, extractor syntax.SampleExtractor) (iter.SampleIterator, error) {
	if err := s.streamReader.SetPredicate(streamsPredicate); err != nil {
		return nil, err
	}
	for _, reader := range s.logReaders {
		if err := reader.SetPredicate(logsPredicate); err != nil {
			return nil, err
		}
	}

	if err := s.matchStreams(ctx); err != nil {
		return nil, err
	}

	iterators := make([]iter.SampleIterator, len(s.logReaders))
	g, ctx := errgroup.WithContext(ctx)

	for i, reader := range s.logReaders {
		g.Go(func() error {
			iter, err := newSampleIterator(ctx, s.streams, extractor, reader)
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

func (s *shardedObject) matchStreams(ctx context.Context) error {
	streamsPtr := streamsPool.Get().(*[]dataobj.Stream)
	defer streamsPool.Put(streamsPtr)
	streams := *streamsPtr

	for {
		n, err := s.streamReader.Read(ctx, streams)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
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
