package indexgateway

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const (
	// ShardingDefault routes each request to one gateway of the tenant's shuffle shard.
	ShardingDefault = "default"
	// ShardingPerIndex splits each request by index table and routes each part
	// to the owners of that (tenant, table) index. Requires ring mode.
	ShardingPerIndex = "per_index"
)

// tablePart is the part of a request that reads one index table.
type tablePart struct {
	Table         string
	From, Through model.Time
}

// splitByTable splits [from, through] into one part per index table of
// tableRange that the index gateway reads to answer it, clipped to the table.
// As through is inclusive, a through exactly at midnight yields a part
// [through, through] for the table starting at that midnight.
func splitByTable(from, through model.Time, tableRange config.TableRange) []tablePart {
	var parts []tablePart
	for _, table := range (config.TableRanges{tableRange}).IndexTablesFor(from, through) {
		parts = append(parts, tablePart{
			Table:   table.Name,
			From:    max(from, table.Start),
			Through: min(through, table.End),
		})
	}
	return parts
}

// perIndex reports whether requests are split by index table: with per_index
// sharding, for a schema period that has a sharded index copy. Requests for
// any other period are routed as with default sharding.
func (s *GatewayClient) perIndex() bool {
	p := s.cfg.TableRange.PeriodConfig
	return s.cfg.Sharding == ShardingPerIndex && p != nil && p.IndexTables.ShardedPathPrefix != ""
}

// shardFactor is the number of fingerprint-range shards the sharded index copy
// of this client's period is split into.
func (s *GatewayClient) shardFactor() int {
	return s.cfg.TableRange.PeriodConfig.IndexTables.ShardFactor
}

// requestPart is the part of a request that reads one shard of one index
// table: the unit of ownership in the index gateway ring.
type requestPart struct {
	tablePart
	Shard index.ShardAnnotation
}

// requestShard returns the fingerprint filter a request's matchers carry as
// the shard label the query planner injects (astmapper.ShardLabel), or nil
// when they carry none. Matchers that fail to parse are treated as carrying
// no shard: the index gateway reports the error.
func requestShard(matchers string) index.FingerprintFilter {
	if matchers == "" {
		return nil
	}
	ms, err := syntax.ParseMatchers(matchers, true)
	if err != nil {
		return nil
	}
	for _, m := range ms {
		if m.Name != astmapper.ShardLabel || m.Type != labels.MatchEqual {
			continue
		}
		shard, _, err := logql.ParseShard(m.Value)
		if err != nil {
			return nil
		}
		return &shard
	}
	return nil
}

// shardsFor returns the shards of the index a request must be sent to: every
// shard of the period's shard factor whose fingerprint range the request's
// filter intersects (one for a power-of-two shard, one or two for a bounded
// one), or every shard when the request carries no filter. An index that is
// not split has the single WholeIndexShard.
func (s *GatewayClient) shardsFor(filter index.FingerprintFilter) []index.ShardAnnotation {
	k := s.shardFactor()
	if filter == nil {
		return IndexShards(k)
	}
	from, through := filter.GetFromThrough()
	if shards := ShardsOverlapping(k, from, through); len(shards) > 0 {
		return shards
	}
	// An empty filter range selects nothing; let one owner answer it.
	return IndexShards(k)[:1]
}

// splitByTableAndShard splits [from, through] into one part per (table, shard)
// the request reads, in table then shard order.
func (s *GatewayClient) splitByTableAndShard(from, through model.Time, filter index.FingerprintFilter) (tables []tablePart, parts []requestPart) {
	tables = splitByTable(from, through, s.cfg.TableRange)
	if len(tables) == 0 {
		return nil, nil
	}
	shards := s.shardsFor(filter)
	parts = make([]requestPart, 0, len(tables)*len(shards))
	for _, t := range tables {
		for _, shard := range shards {
			parts = append(parts, requestPart{tablePart: t, Shard: shard})
		}
	}
	return tables, parts
}

// indexOwnerAddrs returns the addresses of the gateways that own shard of the
// index of tenantID in table.
func (s *GatewayClient) indexOwnerAddrs(tenantID, table string, shard index.ShardAnnotation) ([]string, error) {
	rs, err := s.ownership.Owners(tenantID, table, shard, IndexOwnersRead)
	if err != nil {
		return nil, errors.Wrapf(err, "index gateway get owners of table %s shard %s", table, shard)
	}
	addrs := dedupe(rs.GetAddresses())
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no index gateway instances own table %s shard %s for tenant %s", table, shard, tenantID)
	}
	return addrs, nil
}

// doOnIndexOwners sends one request to the owners of shard of the index of
// tenantID in table, trying them as poolDoWithMaxRetries tries the tenant's
// gateways. Each call takes a slot of the in-flight gate.
func (s *GatewayClient) doOnIndexOwners(
	ctx context.Context,
	tenantID, table string,
	shard index.ShardAnnotation,
	maxRetries int,
	callback func(client logproto.IndexGatewayClient) error,
) error {
	if err := s.inFlight.Start(ctx); err != nil {
		return mapInFlightGateError(err)
	}
	defer s.inFlight.Done()

	addrs, err := s.indexOwnerAddrs(tenantID, table, shard)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to find index gateway owners", "tenant", tenantID, "table", table, "shard", shard, "err", err)
		return err
	}
	return s.tryAddrs(tenantID, addrs, maxRetries, callback)
}

// fanOut splits [from, through] by index table, and each table by the shards
// of the tenant's index the request's fingerprint filter reads, and calls
// call for each part on the owners of its (table, shard), with at most
// PerIndexMaxConcurrency parts in flight. It returns the results in table then
// shard order. The first error cancels the other parts and is returned;
// partial results are never returned.
//
// A request that reads a single part is passed to call unchanged. ok is false
// when the range reads no table of this client's period; the caller should
// then send the request the default way.
func fanOut[T any](
	ctx context.Context,
	s *GatewayClient,
	op string,
	from, through model.Time,
	filter index.FingerprintFilter,
	call func(ctx context.Context, client logproto.IndexGatewayClient, part requestPart) (T, error),
) (results []T, ok bool, err error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, true, errors.Wrap(err, "index gateway client get tenant ID")
	}

	tables, parts := s.splitByTableAndShard(from, through, filter)
	if len(parts) == 0 {
		return nil, false, nil
	}

	s.fanoutMetrics.requestTables.WithLabelValues(op).Observe(float64(len(tables)))
	s.fanoutMetrics.requestParts.WithLabelValues(op).Observe(float64(len(parts)))
	if n := len(tables); n > 1 && tables[n-1].From == through && tables[n-1].Through == through {
		// The last table is read only because through is exactly at its start.
		s.fanoutMetrics.boundaryParts.WithLabelValues(op).Inc()
	}

	if len(parts) == 1 {
		// the request is sent as it is, with its own range
		single := parts[0]
		single.From, single.Through = from, through
		var res T
		err := s.doOnIndexOwners(ctx, tenantID, single.Table, single.Shard, s.cfg.MaxRetries, func(client logproto.IndexGatewayClient) error {
			r, err := call(ctx, client, single)
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
			return s.doOnIndexOwners(gctx, tenantID, p.Table, p.Shard, s.cfg.MaxRetries, func(client logproto.IndexGatewayClient) error {
				r, err := call(gctx, client, p)
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

// narrowMatchers returns matchers restricted to shard: the shard label the
// query planner uses (astmapper.ShardLabel) is set to the intersection of the
// request's fingerprint filter (requestShard, nil for none) and the shard,
// so that the owners of the shard answer for its fingerprint range only. The
// owner may hold other shards of the tenant's index, or a whole index
// downloaded on demand, and would otherwise answer for those too; results
// that are summed rather than deduplicated (stats, volumes) would then count
// them once per part. The matchers are returned as they are for an unsplit
// index (WholeIndexShard), or when they cannot be parsed.
func narrowMatchers(matchers string, filter index.FingerprintFilter, shard index.ShardAnnotation) string {
	if shard.Of < 2 {
		return matchers
	}
	ms, err := syntax.ParseMatchers(matchers, true)
	if err != nil && matchers != "" {
		return matchers
	}

	shardFrom, shardThrough := shardBounds(shard)
	narrowed := shard.String()
	if filter != nil {
		from, through := filter.GetFromThrough()
		if from <= shardFrom && through >= shardThrough {
			// the filter covers the shard: the shard is the narrower range
		} else if p, ok := filter.(*logql.Shard); ok && p.PowerOfTwo != nil {
			// a finer power-of-two shard lies within one shard: keep it
			narrowed = p.PowerOfTwo.String()
		} else {
			// a bounded range crossing a shard boundary: clip it to the shard.
			// Both [from, through) ends are exclusive at the top, except that
			// the maximum fingerprint stands for "no upper bound".
			maxFP := shardThrough - 1
			if shardThrough == model.Fingerprint(math.MaxUint64) {
				maxFP = shardThrough
			}
			filterMax := through - 1
			if through == model.Fingerprint(math.MaxUint64) {
				filterMax = through
			}
			bounded := logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{
				Min: max(from, shardFrom),
				Max: min(filterMax, maxFP),
			}})
			narrowed = bounded.String()
		}
	}

	kept := make([]*labels.Matcher, 0, len(ms)+1)
	for _, m := range ms {
		if m.Name == astmapper.ShardLabel {
			continue
		}
		kept = append(kept, m)
	}
	kept = append(kept, labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, narrowed))
	return syntax.MatchersString(kept)
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
