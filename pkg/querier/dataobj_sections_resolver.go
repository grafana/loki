package querier

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// dataObjSectionsResolver resolves the data-object sections a metric query must read for [start, end].
type dataObjSectionsResolver interface {
	// bucketRange, when non-nil, narrows resolution to that shard-bucket range (honored only by the
	// metastore/postings resolver; the index-gateway resolver ignores it).
	resolveSections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, bucketRange *metastore.ShardBucketRange) ([]*metastore.DataobjSectionDescriptor, error)
}

// metastoreSectionsResolver resolves sections locally via the metastore. This is the default path and
// the fallback when index-gateway resolution is disabled or fails.
type metastoreSectionsResolver struct {
	ms metastore.Metastore
}

func (r metastoreSectionsResolver) resolveSections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, bucketRange *metastore.ShardBucketRange) ([]*metastore.DataobjSectionDescriptor, error) {
	resp, err := r.ms.Sections(ctx, metastore.SectionsRequest{Start: start, End: end, Matchers: matchers, ShardBucketRange: bucketRange})
	if err != nil {
		return nil, err
	}
	return resp.Sections, nil
}

// DataObjSectionsGatewayClient is the subset of the index-gateway client the querier needs to resolve
// data-object sections. *indexgateway.GatewayClient satisfies it.
type DataObjSectionsGatewayClient interface {
	ResolveDataObjectSections(ctx context.Context, in *logproto.ResolveDataObjectSectionsRequest) (*logproto.ResolveDataObjectSectionsResponse, error)
}

// indexGatewaySectionsResolver resolves sections by calling the index-gateway in parallel, once per
// 12h UTC window. It falls back to the metastore on any gateway error so a query never fails or
// under-resolves.
type indexGatewaySectionsResolver struct {
	client    DataObjSectionsGatewayClient
	fallback  dataObjSectionsResolver
	fallbacks prometheus.Counter       // never nil: the fallback path increments it unconditionally
	duration  *prometheus.HistogramVec // may be nil in tests
	logger    log.Logger
}

// newDataObjSectionsResolver returns the gateway-backed resolver when a client is provided (with the
// metastore as fallback), otherwise the plain metastore resolver. reg may be nil.
func newDataObjSectionsResolver(ms metastore.Metastore, client DataObjSectionsGatewayClient, reg prometheus.Registerer, logger log.Logger) dataObjSectionsResolver {
	metastoreResolver := metastoreSectionsResolver{ms: ms}
	if client == nil {
		return metastoreResolver
	}
	fallbacks := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "loki_querier_dataobj_section_resolution_fallbacks_total",
		Help: "Times the querier fell back to local metastore section resolution after an index-gateway error. A steady rate can indicate a disabled or misconfigured gateway.",
	})
	duration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:                            "loki_querier_dataobj_sections_resolve_duration_seconds",
		Help:                            "Time taken by the querier to resolve data-object sections for a query range via the index-gateway, including any fallback to the local metastore.",
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 0,
	}, []string{"outcome"})
	return indexGatewaySectionsResolver{client: client, fallback: metastoreResolver, fallbacks: fallbacks, duration: duration, logger: logger}
}

// bucketRange is ignored on the gateway path: the ResolveDataObjectSections RPC carries no shard, and the
// gateway resolves once per window shared across shards. It is forwarded only to the metastore fallback.
func (r indexGatewaySectionsResolver) resolveSections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, bucketRange *metastore.ShardBucketRange) (out []*metastore.DataobjSectionDescriptor, err error) {
	if r.duration != nil {
		defer func(t0 time.Time) {
			r.duration.WithLabelValues(resolveOutcome(ctx, err)).Observe(time.Since(t0).Seconds())
		}(time.Now())
	}

	matcherStr := syntax.MatchersString(matchers)

	var windows []multitenancy.TimeRange
	for _, tr := range metastore.IterTableOfContentsPaths(start, end) {
		windows = append(windows, tr)
	}

	// Fan out the per-window RPCs with no concurrency cap: they are cheap on the client side (the
	// gateway caches and singleflights the actual work), and a query spans only a handful of windows.
	// responses is indexed by window, so the later merge stays deterministic regardless of the order
	// the calls complete in.
	responses := make([]*logproto.ResolveDataObjectSectionsResponse, len(windows))
	g, gctx := errgroup.WithContext(ctx)
	for i, tr := range windows {
		g.Go(func() error {
			resp, err := r.client.ResolveDataObjectSections(gctx, &logproto.ResolveDataObjectSectionsRequest{
				From:     model.TimeFromUnixNano(tr.MinTime.UnixNano()),
				Through:  model.TimeFromUnixNano(tr.MaxTime.UnixNano()),
				Matchers: matcherStr,
			})
			if err != nil {
				return err
			}
			responses[i] = resp
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		// The caller cancelled or timed out: a metastore fallback would fail the same way, and counting
		// it as a gateway fallback would pollute that signal. Return the cancellation as-is.
		if ctx.Err() != nil {
			return nil, err
		}
		// Any other window failure discards the whole gateway result and re-resolves the full range on
		// the metastore, so a query never under-resolves or double-counts. The fallback runs on the
		// parent ctx, not the errgroup's cancelled child.
		r.fallbacks.Inc()
		level.Warn(r.logger).Log("msg", "index-gateway section resolution failed; falling back to metastore", "err", err)
		return r.fallback.resolveSections(ctx, start, end, matchers, bucketRange)
	}

	// A data object that straddles a 12h boundary is listed in both windows' ToCs, so the same
	// (object, section) can come back from more than one window. Merge by section key and union the
	// stream IDs, mirroring the metastore's own per-SectionKey dedup. Sections keep a deterministic
	// order (window order, then first-seen); each section's stream IDs are a set, so their order is
	// not significant and the read path consumes them as one.
	merged := make(map[metastore.SectionKey]map[int64]struct{})
	var order []metastore.SectionKey
	for _, resp := range responses {
		for i := range resp.Objects {
			obj := &resp.Objects[i]
			for j := range obj.Sections {
				sec := &obj.Sections[j]
				key := metastore.SectionKey{ObjectPath: obj.ObjectPath, SectionIdx: sec.SectionIdx}
				ids, ok := merged[key]
				if !ok {
					ids = make(map[int64]struct{}, len(sec.StreamIds))
					merged[key] = ids
					order = append(order, key)
				}
				for _, id := range sec.StreamIds {
					ids[id] = struct{}{}
				}
			}
		}
	}

	// The read path consumes only ObjectPath, SectionIdx, and StreamIDs from each descriptor, which is
	// all the RPC carries; RowCount, Size, Start, End, and AmbiguousPredicates are intentionally left
	// zero here (as they are in the metastore's postings flow).
	out = make([]*metastore.DataobjSectionDescriptor, 0, len(order))
	for _, key := range order {
		ids := make([]int64, 0, len(merged[key]))
		for id := range merged[key] {
			ids = append(ids, id)
		}
		out = append(out, &metastore.DataobjSectionDescriptor{SectionKey: key, StreamIDs: ids})
	}
	return out, nil
}

// resolveOutcome classifies a resolveSections result for the duration metric's "outcome" label. It
// reports "canceled" only when the caller's context is done (a client cancellation or deadline), so a
// normal query cancellation does not inflate the "error" series alerts watch. Any other failure is a
// real "error".
func resolveOutcome(ctx context.Context, err error) string {
	switch {
	case err == nil:
		return "success"
	case ctx.Err() != nil:
		return "canceled"
	default:
		return "error"
	}
}
