package querier

import (
	"context"
	"flag"
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
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
	return iter.NoopEntryIterator, nil
}

// SelectSamples implements querier.Store
func (s *Store) SelectSamples(_ context.Context, _ logql.SelectSampleParams) (iter.SampleIterator, error) {
	return iter.NoopSampleIterator, nil
}

// SelectSeries implements querier.Store
func (s *Store) SelectSeries(_ context.Context, _ logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	return []logproto.SeriesIdentifier{}, nil
}

// LabelValuesForMetricName implements querier.Store
func (s *Store) LabelValuesForMetricName(_ context.Context, _ string, _ model.Time, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return []string{}, nil
}

// LabelNamesForMetricName implements querier.Store
func (s *Store) LabelNamesForMetricName(_ context.Context, _ string, _ model.Time, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return []string{}, nil
}

// Stats implements querier.Store
func (s *Store) Stats(_ context.Context, _ string, _ model.Time, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return &stats.Stats{}, nil
}

// Volume implements querier.Store
func (s *Store) Volume(_ context.Context, _ string, _ model.Time, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return &logproto.VolumeResponse{}, nil
}

// GetShards implements querier.Store
func (s *Store) GetShards(_ context.Context, _ string, _ model.Time, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return &logproto.ShardsResponse{}, nil
}
