package logqlbench

import (
	"context"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

// metastoreIndexStoragePrefix is where the index objects objtest's Builder.Close writes live
// under the data-object bucket.
const metastoreIndexStoragePrefix = "index/v0"

// newDataObjBucket opens the bucket an objtest.Builder wrote at dir, decorated if decorator is
// set.
func newDataObjBucket(dir string, decorator func(objstore.Bucket) objstore.Bucket) (objstore.Bucket, error) {
	bucket, err := filesystem.NewBucket(filepath.Join(dir, storageDirName, "dataobj"))
	if err != nil {
		return nil, err
	}
	if decorator != nil {
		return decorator(bucket), nil
	}
	return bucket, nil
}

func newDataObjMetastore(bucket objstore.Bucket, logger log.Logger, metrics *metastore.ObjectMetastoreMetrics) *metastore.ObjectMetastore {
	var cfg metastore.Config
	flagext.DefaultValues(&cfg)
	cfg.IndexStoragePrefix = metastoreIndexStoragePrefix
	return metastore.NewObjectMetastore(bucket, cfg, logger, metrics)
}

// unreachableStore implements querier.Store by panicking on every method. DataObjStore embeds a
// Store only to fall back to it for a non-stream-first SelectSamples request; this benchmark's
// dataobj scenario only ever issues stream-first requests, so that fallback, and every other
// Store method DataObjStore promotes unchanged, are never reached. Using this instead of the real
// chunk store keeps the dataobj-only scenario from building one, which the lazy-construction
// design in README.md relies on for an isolated peak-memory measurement.
type unreachableStore struct{}

func (unreachableStore) unreachable(method string) {
	panic("unreachableStore: " + method + " called; DataObjStore should only reach its embedded " +
		"Store for a non-stream-first request, which this benchmark never issues")
}

func (s unreachableStore) SelectSamples(context.Context, logql.SelectSampleParams) (iter.SampleIterator, error) {
	s.unreachable("SelectSamples")
	return nil, nil
}

func (s unreachableStore) SelectLogs(context.Context, logql.SelectLogParams) (iter.EntryIterator, error) {
	s.unreachable("SelectLogs")
	return nil, nil
}

func (s unreachableStore) SelectSeries(context.Context, logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	s.unreachable("SelectSeries")
	return nil, nil
}

func (s unreachableStore) LabelValuesForMetricName(context.Context, string, model.Time, model.Time, string, string, ...*labels.Matcher) ([]string, error) {
	s.unreachable("LabelValuesForMetricName")
	return nil, nil
}

func (s unreachableStore) LabelNamesForMetricName(context.Context, string, model.Time, model.Time, string, ...*labels.Matcher) ([]string, error) {
	s.unreachable("LabelNamesForMetricName")
	return nil, nil
}

func (s unreachableStore) Stats(context.Context, string, model.Time, model.Time, ...*labels.Matcher) (*stats.Stats, error) {
	s.unreachable("Stats")
	return nil, nil
}

func (s unreachableStore) Volume(context.Context, string, model.Time, model.Time, int32, []string, string, ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	s.unreachable("Volume")
	return nil, nil
}

func (s unreachableStore) GetShards(context.Context, string, model.Time, model.Time, uint64, chunk.Predicate) (*logproto.ShardsResponse, error) {
	s.unreachable("GetShards")
	return nil, nil
}
