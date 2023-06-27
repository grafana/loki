package queryrange

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
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
				reqs[end] = &logproto.VolumeRequest{
					From:     model.TimeFromUnix(start.Unix()),
					Through:  model.TimeFromUnix(end.Unix()),
					Matchers: volReq.Matchers,
					Limit:    volReq.Limit,
					Step:     volReq.Step,
				}
			})

			resps := map[time.Time]*VolumeResponse{}
			for bucket, req := range reqs {
				resp, err := next.Do(ctx, req)
				if err != nil {
					return nil, err
				}

				resps[bucket] = resp.(*VolumeResponse)
			}

			promResp := toPrometheusResponse(resps)
			return promResp, nil
		})
	})
}

func toPrometheusResponse(resps map[time.Time]*VolumeResponse) *LokiPromResponse {
	var headers []*definitions.PrometheusResponseHeader
	samplesByName := make(map[string][]logproto.LegacySample)

	for bucket, resp := range resps {
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
