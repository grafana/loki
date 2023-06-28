package queryrange

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/grafana/dskit/concurrency"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/pkg/util"
)

func SeriesVolumeDownstreamHandler(nextRT http.RoundTripper, codec queryrangebase.Codec) queryrangebase.Handler {
	return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		request, err := codec.EncodeRequest(ctx, req)
		if err != nil {
			return nil, err
		}

		if err := user.InjectOrgIDIntoHTTPRequest(ctx, request); err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}

		resp, err := nextRT.RoundTrip(request)
		if err != nil {
			return nil, err
		}

		return codec.DecodeResponse(ctx, resp, req)
	})
}

func NewSeriesVolumeMiddleware() queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			volReq, ok := req.(*logproto.VolumeRequest)

			if !ok {
				return next.Do(ctx, req)
			}

			reqs := map[time.Time]queryrangebase.Request{}
			startTS := volReq.From.Time()
			endTS := volReq.Through.Time()
			interval := time.Duration(volReq.Step * 1e6)

			util.ForInterval(interval, startTS, endTS, true, func(start, end time.Time) {
				// Range query buckets are aligned to the starting timestamp
				// Instant queries are for "this instant", which aligns to the end of the requested range
				bucket := start
				if interval == 0 {
					bucket = end
				}

				reqs[bucket] = &logproto.VolumeRequest{
					From:     model.TimeFromUnix(start.Unix()),
					Through:  model.TimeFromUnix(end.Unix()),
					Matchers: volReq.Matchers,
					Limit:    volReq.Limit,
					Step:     volReq.Step,
				}
			})

			type f func(context.Context) (time.Time, definitions.Response, error)
			var jobs []f

			for bucket, req := range reqs {
				b, r := bucket, req
				jobs = append(jobs, f(func(ctx context.Context) (time.Time, definitions.Response, error) {
					resp, err := next.Do(ctx, r)
					if err != nil {
						return b, nil, err
					}

					return b, resp, nil
				}))
			}

			collector := make(chan *bucketedVolumeResponse, len(jobs))
			defer close(collector)

			err := concurrency.ForEachJob(
				ctx,
				len(jobs),
				len(jobs),
				func(ctx context.Context, i int) error {
					bucket, resp, err := jobs[i](ctx)
					if resp == nil {
						collector <- nil
						return err
					}

					collector <- &bucketedVolumeResponse{
						bucket, resp.(*VolumeResponse),
					}
					return err
				})

			if err != nil {
				return nil, err
			}

			promResp := toPrometheusResponse(collector, len(jobs))
			return promResp, nil
		})
	})
}

type bucketedVolumeResponse struct {
	bucket   time.Time
	response *VolumeResponse
}

func toPrometheusResponse(respsCh chan *bucketedVolumeResponse, totalResponses int) *LokiPromResponse {
	var headers []*definitions.PrometheusResponseHeader
	samplesByName := make(map[string][]logproto.LegacySample)

	for i := 0; i < totalResponses; i++ {
		bucketedVolumeResponse := <-respsCh
		if bucketedVolumeResponse == nil {
			continue
		}

		bucket, resp := bucketedVolumeResponse.bucket, bucketedVolumeResponse.response

		if headers == nil {
			headers := make([]*definitions.PrometheusResponseHeader, len(resp.Headers))
			for i, header := range resp.Headers {
				h := header
				headers[i] = &h
			}
		}

		for _, volume := range resp.Response.Volumes {
			if _, ok := samplesByName[volume.Name]; !ok {
				samplesByName[volume.Name] = make([]logproto.LegacySample, 0, 1)
			}

			samplesByName[volume.Name] = append(samplesByName[volume.Name], toPrometheusSample(volume, bucket))
		}
	}

	promResponse := queryrangebase.PrometheusResponse{
		Status:  loghttp.QueryStatusSuccess,
		Data:    toPrometheusData(samplesByName),
		Headers: headers,
	}

	return &LokiPromResponse{
		Response:   &promResponse,
		Statistics: stats.Result{},
	}
}

func toPrometheusSample(volume logproto.Volume, t time.Time) logproto.LegacySample {
	ts := model.TimeFromUnix(t.Unix())
	return logproto.LegacySample{
		Value:       float64(volume.Volume),
		TimestampMs: ts.UnixNano() / 1e6,
	}
}

type sortableSampleStream struct {
	labels  labels.Labels
	samples []logproto.LegacySample
}

func toPrometheusData(series map[string][]logproto.LegacySample) queryrangebase.PrometheusData {
	resultType := loghttp.ResultTypeVector
	sortableResult := make([]sortableSampleStream, 0, len(series))

	for name, samples := range series {
		if resultType == loghttp.ResultTypeVector && len(samples) > 1 {
			resultType = loghttp.ResultTypeMatrix
		}

		lbls, err := syntax.ParseLabels(name)
		if err != nil {
			continue
		}

		sort.Slice(samples, func(i, j int) bool {
			return samples[i].TimestampMs < samples[j].TimestampMs
		})

		sortableResult = append(sortableResult, sortableSampleStream{
			labels:  lbls,
			samples: samples,
		})
	}

	sort.Slice(sortableResult, func(i, j int) bool {
		return sortableResult[i].labels.String() < sortableResult[j].labels.String()
	})

	result := make([]queryrangebase.SampleStream, 0, len(sortableResult))
	for _, r := range sortableResult {
		result = append(result, queryrangebase.SampleStream{
			Labels:  logproto.FromLabelsToLabelAdapters(r.labels),
			Samples: r.samples,
		})
	}

	return queryrangebase.PrometheusData{
		ResultType: resultType,
		Result:     result,
	}
}
