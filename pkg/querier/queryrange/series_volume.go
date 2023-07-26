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
	"github.com/grafana/loki/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/pkg/util"
)

func VolumeDownstreamHandler(nextRT http.RoundTripper, codec queryrangebase.Codec) queryrangebase.Handler {
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

func NewVolumeMiddleware() queryrangebase.Middleware {
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
					From:         model.TimeFromUnix(start.Unix()),
					Through:      model.TimeFromUnix(end.Unix()),
					Matchers:     volReq.Matchers,
					Limit:        volReq.Limit,
					Step:         volReq.Step,
					TargetLabels: volReq.TargetLabels,
					AggregateBy:  volReq.AggregateBy,
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
			close(collector)

			if err != nil {
				return nil, err
			}

			promResp := ToPrometheusResponse(collector, seriesvolume.AggregateBySeries(volReq.AggregateBy))
			return promResp, nil
		})
	})
}

type bucketedVolumeResponse struct {
	bucket   time.Time
	response *VolumeResponse
}

func ToPrometheusResponse(respsCh chan *bucketedVolumeResponse, aggregateBySeries bool) *LokiPromResponse {
	var headers []*definitions.PrometheusResponseHeader
	samplesByName := make(map[string][]logproto.LegacySample)

	for bucketedVolumeResponse := range respsCh {
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
		Data:    toPrometheusData(samplesByName, aggregateBySeries),
		Headers: headers,
	}

	return &LokiPromResponse{
		Response:   &promResponse,
		Statistics: stats.Result{},
	}
}

func toPrometheusSample(volume logproto.Volume, t time.Time) logproto.LegacySample {
	ts := model.TimeFromUnixNano(t.UnixNano())
	return logproto.LegacySample{
		Value:       float64(volume.Volume),
		TimestampMs: ts.UnixNano() / 1e6,
	}
}

type sortableSampleStream struct {
	name    string
	labels  labels.Labels
	samples []logproto.LegacySample
}

func toPrometheusData(series map[string][]logproto.LegacySample, aggregateBySeries bool) queryrangebase.PrometheusData {
	resultType := loghttp.ResultTypeVector
	sortableResult := make([]sortableSampleStream, 0, len(series))

	for name, samples := range series {
		if resultType == loghttp.ResultTypeVector && len(samples) > 1 {
			resultType = loghttp.ResultTypeMatrix
		}

		var lbls labels.Labels
		var err error

		if aggregateBySeries {
			lbls, err = syntax.ParseLabels(name)
		} else {
			lbls = labels.Labels{{
				Name:  name,
				Value: "",
			}}
		}

		if err != nil {
			continue
		}

		sort.Slice(samples, func(i, j int) bool {
			return samples[i].TimestampMs < samples[j].TimestampMs
		})

		sortableResult = append(sortableResult, sortableSampleStream{
			name:    name,
			labels:  lbls,
			samples: samples,
		})
	}

	sort.Slice(sortableResult, func(i, j int) bool {
		// Sorting by value only helps instant queries so just grab the first value
		if sortableResult[i].samples[0].Value == sortableResult[j].samples[0].Value {
			return sortableResult[i].name < sortableResult[j].name
		}
		return sortableResult[i].samples[0].Value > sortableResult[j].samples[0].Value
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
