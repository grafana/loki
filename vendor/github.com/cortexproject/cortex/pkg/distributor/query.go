package distributor

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/instrument"
	"go.uber.org/atomic"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	ingester_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
	grpc_util "github.com/cortexproject/cortex/pkg/util/grpc"
	"github.com/cortexproject/cortex/pkg/util/limiter"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

var (
	errMaxChunksPerQueryLimit = "the query hit the max number of chunks limit while fetching chunks from ingesters for %s (limit: %d)"
)

// Query multiple ingesters and returns a Matrix of samples.
func (d *Distributor) Query(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (model.Matrix, error) {
	var matrix model.Matrix
	err := instrument.CollectedRequest(ctx, "Distributor.Query", d.queryDuration, instrument.ErrorCode, func(ctx context.Context) error {
		req, err := ingester_client.ToQueryRequest(from, to, matchers)
		if err != nil {
			return err
		}

		replicationSet, err := d.GetIngestersForQuery(ctx, matchers...)
		if err != nil {
			return err
		}

		matrix, err = d.queryIngesters(ctx, replicationSet, req)
		if err != nil {
			return err
		}

		if s := opentracing.SpanFromContext(ctx); s != nil {
			s.LogKV("series", len(matrix))
		}
		return nil
	})
	return matrix, err
}

// QueryStream multiple ingesters via the streaming interface and returns big ol' set of chunks.
func (d *Distributor) QueryStream(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (*ingester_client.QueryStreamResponse, error) {
	var result *ingester_client.QueryStreamResponse
	err := instrument.CollectedRequest(ctx, "Distributor.QueryStream", d.queryDuration, instrument.ErrorCode, func(ctx context.Context) error {
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			return err
		}

		req, err := ingester_client.ToQueryRequest(from, to, matchers)
		if err != nil {
			return err
		}

		replicationSet, err := d.GetIngestersForQuery(ctx, matchers...)
		if err != nil {
			return err
		}

		result, err = d.queryIngesterStream(ctx, userID, replicationSet, req)
		if err != nil {
			return err
		}

		if s := opentracing.SpanFromContext(ctx); s != nil {
			s.LogKV("chunk-series", len(result.GetChunkseries()), "time-series", len(result.GetTimeseries()))
		}
		return nil
	})
	return result, err
}

// GetIngestersForQuery returns a replication set including all ingesters that should be queried
// to fetch series matching input label matchers.
func (d *Distributor) GetIngestersForQuery(ctx context.Context, matchers ...*labels.Matcher) (ring.ReplicationSet, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return ring.ReplicationSet{}, err
	}

	// If shuffle sharding is enabled we should only query ingesters which are
	// part of the tenant's subring.
	if d.cfg.ShardingStrategy == util.ShardingStrategyShuffle {
		shardSize := d.limits.IngestionTenantShardSize(userID)
		lookbackPeriod := d.cfg.ShuffleShardingLookbackPeriod

		if shardSize > 0 && lookbackPeriod > 0 {
			return d.ingestersRing.ShuffleShardWithLookback(userID, shardSize, lookbackPeriod, time.Now()).GetReplicationSetForOperation(ring.Read)
		}
	}

	// If "shard by all labels" is disabled, we can get ingesters by metricName if exists.
	if !d.cfg.ShardByAllLabels {
		metricNameMatcher, _, ok := extract.MetricNameMatcherFromMatchers(matchers)

		if ok && metricNameMatcher.Type == labels.MatchEqual {
			return d.ingestersRing.Get(shardByMetricName(userID, metricNameMatcher.Value), ring.Read, nil, nil, nil)
		}
	}

	return d.ingestersRing.GetReplicationSetForOperation(ring.Read)
}

// GetIngestersForMetadata returns a replication set including all ingesters that should be queried
// to fetch metadata (eg. label names/values or series).
func (d *Distributor) GetIngestersForMetadata(ctx context.Context) (ring.ReplicationSet, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return ring.ReplicationSet{}, err
	}

	// If shuffle sharding is enabled we should only query ingesters which are
	// part of the tenant's subring.
	if d.cfg.ShardingStrategy == util.ShardingStrategyShuffle {
		shardSize := d.limits.IngestionTenantShardSize(userID)
		lookbackPeriod := d.cfg.ShuffleShardingLookbackPeriod

		if shardSize > 0 && lookbackPeriod > 0 {
			return d.ingestersRing.ShuffleShardWithLookback(userID, shardSize, lookbackPeriod, time.Now()).GetReplicationSetForOperation(ring.Read)
		}
	}

	return d.ingestersRing.GetReplicationSetForOperation(ring.Read)
}

// queryIngesters queries the ingesters via the older, sample-based API.
func (d *Distributor) queryIngesters(ctx context.Context, replicationSet ring.ReplicationSet, req *ingester_client.QueryRequest) (model.Matrix, error) {
	// Fetch samples from multiple ingesters in parallel, using the replicationSet
	// to deal with consistency.
	results, err := replicationSet.Do(ctx, d.cfg.ExtraQueryDelay, func(ctx context.Context, ing *ring.InstanceDesc) (interface{}, error) {
		client, err := d.ingesterPool.GetClientFor(ing.Addr)
		if err != nil {
			return nil, err
		}

		resp, err := client.(ingester_client.IngesterClient).Query(ctx, req)
		d.ingesterQueries.WithLabelValues(ing.Addr).Inc()
		if err != nil {
			d.ingesterQueryFailures.WithLabelValues(ing.Addr).Inc()
			return nil, err
		}

		return ingester_client.FromQueryResponse(resp), nil
	})
	if err != nil {
		return nil, err
	}

	// Merge the results into a single matrix.
	fpToSampleStream := map[model.Fingerprint]*model.SampleStream{}
	for _, result := range results {
		for _, ss := range result.(model.Matrix) {
			fp := ss.Metric.Fingerprint()
			mss, ok := fpToSampleStream[fp]
			if !ok {
				mss = &model.SampleStream{
					Metric: ss.Metric,
				}
				fpToSampleStream[fp] = mss
			}
			mss.Values = util.MergeSampleSets(mss.Values, ss.Values)
		}
	}
	result := model.Matrix{}
	for _, ss := range fpToSampleStream {
		result = append(result, ss)
	}

	return result, nil
}

// queryIngesterStream queries the ingesters using the new streaming API.
func (d *Distributor) queryIngesterStream(ctx context.Context, userID string, replicationSet ring.ReplicationSet, req *ingester_client.QueryRequest) (*ingester_client.QueryStreamResponse, error) {
	var (
		chunksLimit  = d.limits.MaxChunksPerQueryFromIngesters(userID)
		chunksCount  = atomic.Int32{}
		queryLimiter = limiter.QueryLimiterFromContextWithFallback(ctx)
	)

	// Fetch samples from multiple ingesters
	results, err := replicationSet.Do(ctx, d.cfg.ExtraQueryDelay, func(ctx context.Context, ing *ring.InstanceDesc) (interface{}, error) {
		client, err := d.ingesterPool.GetClientFor(ing.Addr)
		if err != nil {
			return nil, err
		}
		d.ingesterQueries.WithLabelValues(ing.Addr).Inc()

		stream, err := client.(ingester_client.IngesterClient).QueryStream(ctx, req)
		if err != nil {
			d.ingesterQueryFailures.WithLabelValues(ing.Addr).Inc()
			return nil, err
		}
		defer stream.CloseSend() //nolint:errcheck

		result := &ingester_client.QueryStreamResponse{}
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			} else if err != nil {
				// Do not track a failure if the context was canceled.
				if !grpc_util.IsGRPCContextCanceled(err) {
					d.ingesterQueryFailures.WithLabelValues(ing.Addr).Inc()
				}

				return nil, err
			}

			// Enforce the max chunks limits.
			if chunksLimit > 0 {
				if count := int(chunksCount.Add(int32(resp.ChunksCount()))); count > chunksLimit {
					// We expect to be always able to convert the label matchers back to Prometheus ones.
					// In case we fail (unexpected) the error will not include the matchers, but the core
					// logic doesn't break.
					matchers, _ := ingester_client.FromLabelMatchers(req.Matchers)
					return nil, validation.LimitError(fmt.Sprintf(errMaxChunksPerQueryLimit, util.LabelMatchersToString(matchers), chunksLimit))
				}
			}
			for _, series := range resp.Chunkseries {
				if limitErr := queryLimiter.AddSeries(series.Labels); limitErr != nil {
					return nil, limitErr
				}
			}
			for _, series := range resp.Timeseries {
				if limitErr := queryLimiter.AddSeries(series.Labels); limitErr != nil {
					return nil, limitErr
				}
			}

			result.Chunkseries = append(result.Chunkseries, resp.Chunkseries...)
			result.Timeseries = append(result.Timeseries, resp.Timeseries...)
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}

	hashToChunkseries := map[string]ingester_client.TimeSeriesChunk{}
	hashToTimeSeries := map[string]cortexpb.TimeSeries{}

	for _, result := range results {
		response := result.(*ingester_client.QueryStreamResponse)

		// Parse any chunk series
		for _, series := range response.Chunkseries {
			key := ingester_client.LabelsToKeyString(cortexpb.FromLabelAdaptersToLabels(series.Labels))
			existing := hashToChunkseries[key]
			existing.Labels = series.Labels
			existing.Chunks = append(existing.Chunks, series.Chunks...)
			hashToChunkseries[key] = existing
		}

		// Parse any time series
		for _, series := range response.Timeseries {
			key := ingester_client.LabelsToKeyString(cortexpb.FromLabelAdaptersToLabels(series.Labels))
			existing := hashToTimeSeries[key]
			existing.Labels = series.Labels
			if existing.Samples == nil {
				existing.Samples = series.Samples
			} else {
				existing.Samples = mergeSamples(existing.Samples, series.Samples)
			}
			hashToTimeSeries[key] = existing
		}
	}

	resp := &ingester_client.QueryStreamResponse{
		Chunkseries: make([]ingester_client.TimeSeriesChunk, 0, len(hashToChunkseries)),
		Timeseries:  make([]cortexpb.TimeSeries, 0, len(hashToTimeSeries)),
	}
	for _, series := range hashToChunkseries {
		resp.Chunkseries = append(resp.Chunkseries, series)
	}
	for _, series := range hashToTimeSeries {
		resp.Timeseries = append(resp.Timeseries, series)
	}

	return resp, nil
}

// Merges and dedupes two sorted slices with samples together.
func mergeSamples(a, b []cortexpb.Sample) []cortexpb.Sample {
	if sameSamples(a, b) {
		return a
	}

	result := make([]cortexpb.Sample, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].TimestampMs < b[j].TimestampMs {
			result = append(result, a[i])
			i++
		} else if a[i].TimestampMs > b[j].TimestampMs {
			result = append(result, b[j])
			j++
		} else {
			result = append(result, a[i])
			i++
			j++
		}
	}
	// Add the rest of a or b. One of them is empty now.
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}

func sameSamples(a, b []cortexpb.Sample) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
