package queryrange

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type splitByShardLabel struct {
	limits        Limits
	iqo           util.IngesterQueryOptions
	targetBuckets int
	labelsHandler queryrangebase.Handler
}

func newShardLabelSplitter(
	limits Limits,
	iqo util.IngesterQueryOptions,
	targetBuckets int,
	labelsHandler queryrangebase.Handler,
) *splitByShardLabel {
	return &splitByShardLabel{
		limits:        limits,
		iqo:           iqo,
		targetBuckets: targetBuckets,
		labelsHandler: labelsHandler,
	}
}

func (s splitByShardLabel) buildFactory(
	ctx context.Context,
	req definitions.Request,
	shardedRequests *shardedRequests,
) func(start, end time.Time) {
	defaultFactory := func(req definitions.Request) func(start, end time.Time) {
		switch r := req.(type) {
		case *logproto.IndexStatsRequest:
			return func(start, end time.Time) {
				startTime := model.TimeFromUnixNano(start.UnixNano())
				endTime := model.TimeFromUnixNano(end.UnixNano())
				shardedRequests.reqs = append(shardedRequests.reqs, &logproto.IndexStatsRequest{
					Matchers: r.GetMatchers(),
					From:     startTime,
					Through:  endTime,
				})
			}
		case *logproto.VolumeRequest:
			return func(start, end time.Time) {
				shardedRequests.reqs = append(shardedRequests.reqs, &logproto.VolumeRequest{
					From:         r.From,
					Through:      r.Through,
					Matchers:     r.GetMatchers(),
					Limit:        r.Limit,
					TargetLabels: r.TargetLabels,
					AggregateBy:  r.AggregateBy,
				})
			}
		default:
			return func(start, end time.Time) {
				level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("splitter: unsupported request type: %T", req))
			}
		}
	}

	matchers, err := syntax.ParseMatchers(req.GetQuery(), false)
	if err != nil {
		return defaultFactory(req)
	}

	for _, m := range matchers {
		if m.Name == ingester.ShardLbName {
			return defaultFactory(req)
		}
	}

	origStart := req.GetStart()
	origEnd := req.GetEnd()
	resp, err := s.labelsHandler.Do(ctx, &LabelRequest{
		LabelRequest: logproto.LabelRequest{
			Name:   "__stream_shard__",
			Values: true,
			Start:  &origStart,
			End:    &origEnd,
			Query:  req.GetQuery(),
		},
	})
	if err != nil {
		return defaultFactory(req)
	}

	casted, ok := resp.(*LokiLabelNamesResponse)
	if !ok {
		return defaultFactory(req)
	}

	var maxValue int
	for _, value := range casted.Data {
		shardNum, err := strconv.Atoi(value)
		if err != nil {
			continue
		}

		if maxValue < shardNum {
			maxValue = shardNum
		}
	}

	if maxValue == 0 {
		return defaultFactory(req)
	}

	return func(start, end time.Time) {
		//leave the last bucket for streams without shards
		target := s.targetBuckets - 1

		firstShard := 0
		bucketSize := maxValue / target
		if maxValue%(target) != 0 {
			bucketSize++
		}

		for i := 0; i < target; i++ {
			lastShard := firstShard + bucketSize
			if lastShard > (maxValue + 1) {
				lastShard = maxValue + 1
			}

			matcher := []byte{'('}
			for j := firstShard; j < lastShard; j++ {
				if j > maxValue {
					break
				}
				matcher = append(matcher, []byte(fmt.Sprintf("%d|", j))...)
			}

			if len(matcher) == 1 {
				break
			}

			//last character is '|', replace it with ')' to complete the regex
			matcher[len(matcher)-1] = ')'

			iterationMatchers := append(matchers, &labels.Matcher{
				Type:  labels.MatchRegexp,
				Name:  ingester.ShardLbName,
				Value: string(matcher),
			})

			shardedRequests.append(req, iterationMatchers, start, end)

			firstShard = lastShard
		}

		// Catch all remaining streams without a shard
		iterationMatchers := append(matchers, &labels.Matcher{
			Type:  labels.MatchEqual,
			Name:  ingester.ShardLbName,
			Value: "",
		})
		shardedRequests.append(req, iterationMatchers, start, end)
	}
}

// split implements splitter.
func (s splitByShardLabel) split(
	ctx context.Context,
	execTime time.Time,
	tenantIDs []string,
	req definitions.Request,
	interval time.Duration,
) ([]definitions.Request, error) {
	endTimeInclusive := true
	shardedReqs := shardedRequests{
		reqs: make([]definitions.Request, 0),
	}

	var (
		splitsBeforeRebound []queryrangebase.Request
		origStart           = req.GetStart().UTC()
		origEnd             = req.GetEnd().UTC()
		start, end          = origStart, origEnd

		reboundOrigQuery           bool
		splitIntervalBeforeRebound time.Duration
	)

	switch req.(type) {
	case *logproto.IndexStatsRequest, *logproto.VolumeRequest:
		var (
			recentMetadataQueryWindow        = validation.MaxDurationOrZeroPerTenant(tenantIDs, s.limits.RecentMetadataQueryWindow)
			recentMetadataQuerySplitInterval = validation.MaxDurationOrZeroPerTenant(tenantIDs, s.limits.RecentMetadataQuerySplitDuration)
		)

		// if either of them are not configured, we fallback to the default split interval for the entire query length.
		if recentMetadataQueryWindow == 0 || recentMetadataQuerySplitInterval == 0 {
			break
		}

		start, end, reboundOrigQuery = recentMetadataQueryBounds(execTime, recentMetadataQueryWindow, req)
		splitIntervalBeforeRebound = recentMetadataQuerySplitInterval
	default:
		if ingesterQueryInterval := validation.MaxDurationOrZeroPerTenant(tenantIDs, s.limits.IngesterQuerySplitDuration); ingesterQueryInterval != 0 {
			start, end, reboundOrigQuery = ingesterQueryBounds(execTime, s.iqo, req)
			splitIntervalBeforeRebound = ingesterQueryInterval
		}
	}

	factory := s.buildFactory(ctx, req, &shardedReqs)
	if reboundOrigQuery {
		util.ForInterval(splitIntervalBeforeRebound, start, end, endTimeInclusive, factory)

		// rebound after query portion within ingester query window or recent metadata query window has been split out
		end = start
		start = origStart
		if endTimeInclusive {
			end = end.Add(-util.SplitGap)
		}

		// query only overlaps ingester query window or recent metadata query window, nothing more to do
		if start.After(end) || start.Equal(end) {
			return shardedReqs.reqs, nil
		}

		// copy the splits, reset the results
		splitsBeforeRebound = shardedReqs.reqs
		shardedReqs.reqs = nil
	} else {
		start = origStart
		end = origEnd
	}

	util.ForInterval(interval, start, end, endTimeInclusive, factory)

	reqs := append(shardedReqs.reqs, splitsBeforeRebound...)
	return reqs, nil
}

type shardedRequests struct {
	reqs []definitions.Request
}

func (s *shardedRequests) append(
	req definitions.Request,
	iterationMatchers []*labels.Matcher,
	start, end time.Time,
) {
	switch r := req.(type) {
	case *logproto.IndexStatsRequest:
		s.reqs = append(s.reqs, &logproto.IndexStatsRequest{
			From:     model.TimeFromUnixNano(start.UnixNano()),
			Through:  model.TimeFromUnixNano(end.UnixNano()),
			Matchers: syntax.MatchersString(iterationMatchers),
		})
	case *logproto.VolumeRequest:
		s.reqs = append(s.reqs, &logproto.VolumeRequest{
			From:         r.From,
			Through:      r.Through,
			Matchers:     syntax.MatchersString(iterationMatchers),
			Limit:        r.Limit,
			TargetLabels: r.TargetLabels,
			AggregateBy:  r.AggregateBy,
		})
	}
}
