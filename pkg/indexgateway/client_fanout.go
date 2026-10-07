package indexgateway

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
)

const (
	// ShardingDefault routes each request to one gateway of the tenant's shuffle shard.
	ShardingDefault = "default"
	// ShardingPerIndex splits each request by index table and routes each part
	// to the owners of that (tenant, table) index. Requires ring mode.
	ShardingPerIndex = "per_index"
)

// tablePeriod is the period of every TSDB index table.
const tablePeriod = config.ObjectStorageIndexRequiredPeriod

// tablePart is the part of a request that reads one index table.
type tablePart struct {
	Table         string
	From, Through model.Time
}

// splitByTable splits [from, through] into one part per index table that the
// index gateway reads to answer it. Tables are picked the way tsdb.IndexBuckets
// picks them: every table whose day overlaps [from, through], both ends
// inclusive, that lies within tableRange. Each part is clipped to its table's
// day. As through is inclusive, a through exactly at midnight yields a part
// [through, through] for the table starting at that midnight.
func splitByTable(from, through model.Time, tableRange config.TableRange) []tablePart {
	period := int64(tablePeriod)
	start := from.Time().UnixNano() / period
	end := through.Time().UnixNano() / period

	var parts []tablePart
	for n := start; n <= end; n++ {
		cfg := tableRange.ConfigForTableNumber(n)
		if cfg == nil {
			continue
		}
		dayStart := model.TimeFromUnixNano(n * period)
		dayEnd := model.TimeFromUnixNano((n+1)*period) - 1
		parts = append(parts, tablePart{
			Table:   cfg.IndexTables.Prefix + strconv.FormatInt(n, 10),
			From:    max(from, dayStart),
			Through: min(through, dayEnd),
		})
	}
	return parts
}

// perIndex reports whether requests are split by index table.
func (s *GatewayClient) perIndex() bool {
	return s.cfg.Sharding == ShardingPerIndex
}

// indexOwnerAddrs returns the addresses of the gateways that own the index of
// tenantID in table.
func (s *GatewayClient) indexOwnerAddrs(tenantID, table string) ([]string, error) {
	rs, err := s.ownership.Owners(tenantID, table, IndexOwnersRead)
	if err != nil {
		return nil, errors.Wrapf(err, "index gateway get owners of table %s", table)
	}
	addrs := dedupe(rs.GetAddresses())
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no index gateway instances own table %s for tenant %s", table, tenantID)
	}
	return addrs, nil
}

// doOnIndexOwners sends one request to the owners of the index of tenantID in
// table, trying them as poolDoWithMaxRetries tries the tenant's gateways. Each
// call takes a slot of the in-flight gate.
func (s *GatewayClient) doOnIndexOwners(
	ctx context.Context,
	tenantID, table string,
	maxRetries int,
	callback func(client logproto.IndexGatewayClient) error,
) error {
	if err := s.inFlight.Start(ctx); err != nil {
		return mapInFlightGateError(err)
	}
	defer s.inFlight.Done()

	addrs, err := s.indexOwnerAddrs(tenantID, table)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to find index gateway owners", "tenant", tenantID, "table", table, "err", err)
		return err
	}
	return s.tryAddrs(tenantID, addrs, maxRetries, callback)
}

// fanOut splits [from, through] by index table and calls call for each part
// on the owners of its table, with at most PerIndexMaxConcurrency parts in
// flight. It returns the results in table order. The first error cancels the
// other parts and is returned; partial results are never returned.
//
// A request that reads a single table is passed to call unchanged. ok is false
// when the range reads no table of this client's period; the caller should
// then send the request the default way.
func fanOut[T any](
	ctx context.Context,
	s *GatewayClient,
	op string,
	from, through model.Time,
	call func(ctx context.Context, client logproto.IndexGatewayClient, from, through model.Time) (T, error),
) (results []T, ok bool, err error) {
	parts := splitByTable(from, through, s.cfg.TableRange)
	if len(parts) == 0 {
		return nil, false, nil
	}

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, true, errors.Wrap(err, "index gateway client get tenant ID")
	}

	s.fanoutMetrics.requestTables.WithLabelValues(op).Observe(float64(len(parts)))
	if n := len(parts); n > 1 && parts[n-1].From == through && parts[n-1].Through == through {
		// The last table is read only because through is exactly at its start.
		s.fanoutMetrics.boundaryParts.WithLabelValues(op).Inc()
	}

	if len(parts) == 1 {
		var res T
		err := s.doOnIndexOwners(ctx, tenantID, parts[0].Table, s.cfg.MaxRetries, func(client logproto.IndexGatewayClient) error {
			r, err := call(ctx, client, from, through)
			if err != nil {
				return err
			}
			res = r
			return nil
		})
		if err != nil {
			return nil, true, err
		}
		return []T{res}, true, nil
	}

	results = make([]T, len(parts))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.cfg.PerIndexMaxConcurrency)
	for i, p := range parts {
		g.Go(func() error {
			// Don't start parts once another one has failed.
			if err := gctx.Err(); err != nil {
				return err
			}
			return s.doOnIndexOwners(gctx, tenantID, p.Table, s.cfg.MaxRetries, func(client logproto.IndexGatewayClient) error {
				r, err := call(gctx, client, p.From, p.Through)
				if err != nil {
					return err
				}
				// Only keep the result of a successful attempt.
				results[i] = r
				return nil
			})
		})
	}
	if err := g.Wait(); err != nil {
		return nil, true, err
	}
	return results, true, nil
}

// mergeChunkRefResponses concatenates the chunk refs of resps, dropping
// duplicates: a chunk that spans midnight is indexed in both tables. Stats are
// summed, with TotalChunks and PostFilterChunks corrected for the dropped
// duplicates. TotalStreams stays summed, so it counts a stream once per table
// it was found in.
func mergeChunkRefResponses(resps []*logproto.GetChunkRefResponse) *logproto.GetChunkRefResponse {
	if len(resps) == 1 {
		return resps[0]
	}

	var total int
	for _, r := range resps {
		total += len(r.Refs)
	}

	out := &logproto.GetChunkRefResponse{Refs: make([]*logproto.ChunkRef, 0, total)}
	seen := make(map[logproto.ChunkRef]struct{}, total)
	var duplicates int64
	for _, r := range resps {
		out.Stats.Merge(r.Stats)
		for _, ref := range r.Refs {
			if _, ok := seen[*ref]; ok {
				duplicates++
				continue
			}
			seen[*ref] = struct{}{}
			out.Refs = append(out.Refs, ref)
		}
	}
	out.Stats.TotalChunks -= duplicates
	out.Stats.PostFilterChunks = int64(len(out.Refs))
	return out
}

// mergeSeriesResponses returns the union of the series of resps.
func mergeSeriesResponses(resps []*logproto.GetSeriesResponse) *logproto.GetSeriesResponse {
	if len(resps) == 1 {
		return resps[0]
	}

	out := &logproto.GetSeriesResponse{}
	seen := make(map[uint64][]labels.Labels)
	for _, r := range resps {
		for _, s := range r.Series {
			ls := logproto.FromLabelAdaptersToLabels(s.Labels)
			h := labels.StableHash(ls)
			if slices.ContainsFunc(seen[h], func(other labels.Labels) bool { return labels.Equal(ls, other) }) {
				continue
			}
			seen[h] = append(seen[h], ls)
			out.Series = append(out.Series, s)
		}
	}
	return out
}

// mergeLabelResponses returns the sorted union of the values of resps.
func mergeLabelResponses(resps []*logproto.LabelResponse) *logproto.LabelResponse {
	// MergeLabelResponses never returns an error.
	merged, _ := logproto.MergeLabelResponses(resps)
	return merged
}

// mergeStatsResponses sums the stats of resps. A stream found in more than
// one table is counted once per table.
//
// It can also count slightly less than one request over the whole range when
// through is exactly at midnight. TSDB weighs each chunk by the share of its
// time range inside the query, and the part for the table starting at through
// is a single millisecond, so chunks that start there count for nothing. One
// request counts them, with next to no bytes or entries.
func mergeStatsResponses(resps []*logproto.IndexStatsResponse) *logproto.IndexStatsResponse {
	if len(resps) == 1 {
		return resps[0]
	}

	out := &logproto.IndexStatsResponse{}
	for _, r := range resps {
		if r == nil {
			continue
		}
		out.Streams += r.Streams
		out.Chunks += r.Chunks
		out.Bytes += r.Bytes
		out.Entries += r.Entries
	}
	return out
}

// mergeVolumeResponses sums the volumes of resps per name and keeps the top
// limit. Each part is already limited to its own top limit, so a name that is
// never in the top of any one table is missing. As with stats, a part that
// is a single millisecond at midnight counts nothing for the chunks starting
// there.
func mergeVolumeResponses(resps []*logproto.VolumeResponse, limit int32) *logproto.VolumeResponse {
	if len(resps) == 1 {
		return resps[0]
	}
	return seriesvolume.Merge(resps, limit)
}
