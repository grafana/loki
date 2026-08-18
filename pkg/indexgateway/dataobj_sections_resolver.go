package indexgateway

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// dataObjectSectionsResolveTimeout bounds the shared singleflight resolution. That work runs on a
// context detached from the leader's request, so it needs its own upper bound. It is generous: it
// only guards against a stuck object-store call, not slow-but-progressing resolution.
const dataObjectSectionsResolveTimeout = 30 * time.Second

// DataObjectSectionsResolver resolves the data-object sections matching a query within a single 12h
// UTC-aligned window. It is the server-side implementation behind the
// IndexGateway.ResolveDataObjectSections RPC.
//
// It reuses the existing metastore for index access: GetIndexes lists the window's index objects to
// derive the immutable cache key, and Sections does the expensive postings resolution. Only the
// Sections result is cached; a singleflight collapses concurrent identical resolutions.
type DataObjectSectionsResolver struct {
	metastore metastore.Metastore
	cache     *dataObjectSectionsCache
	sf        singleflight.Group
	logger    log.Logger

	cacheHits prometheus.Counter
	cacheMiss prometheus.Counter
	duration  *prometheus.HistogramVec
}

// NewDataObjectSectionsResolver builds a resolver over ms, constructing its cache from cfg via
// cache.New (embedded L1 + optional memcached L2, tiered). It keeps the cache type internal to the
// package so callers only supply the metastore.
func NewDataObjectSectionsResolver(ms metastore.Metastore, cfg DataObjectSectionsConfig, reg prometheus.Registerer, logger log.Logger) (*DataObjectSectionsResolver, error) {
	c, err := cache.New(cfg.Cache, reg, logger, dataObjSectionsCacheType, constants.Loki)
	if err != nil {
		return nil, err
	}
	return newDataObjectSectionsResolver(ms, newDataObjectSectionsCache(c, logger), reg, logger), nil
}

// newDataObjectSectionsResolver builds a resolver over the given metastore and cache. reg may be nil.
func newDataObjectSectionsResolver(ms metastore.Metastore, cache *dataObjectSectionsCache, reg prometheus.Registerer, logger log.Logger) *DataObjectSectionsResolver {
	return &DataObjectSectionsResolver{
		metastore: ms,
		cache:     cache,
		logger:    logger,
		cacheHits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_index_gateway_dataobj_sections_cache_hits_total",
			Help: "Data-object section resolutions served from the resolution cache (either layer).",
		}),
		cacheMiss: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_index_gateway_dataobj_sections_cache_misses_total",
			Help: "Data-object section resolutions that had to run the metastore lookup.",
		}),
		duration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_index_gateway_dataobj_sections_resolve_duration_seconds",
			Help:                            "Time taken to resolve data-object sections for one window on the index-gateway, including cache lookups.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"outcome"}),
	}
}

// Resolve returns the sections matching matchers in the window [from, through). The window must be a
// single MetastoreWindowSize (12h) UTC-aligned window; an unaligned window is a client bug and
// returns codes.InvalidArgument.
func (r *DataObjectSectionsResolver) Resolve(ctx context.Context, tenant string, from, through model.Time, matchers string) (resp *logproto.ResolveDataObjectSectionsResponse, err error) {
	defer func(start time.Time) {
		r.duration.WithLabelValues(resolveOutcome(ctx, err)).Observe(time.Since(start).Seconds())
	}(time.Now())

	fromT := from.Time().UTC()
	throughT := through.Time().UTC()
	if !fromT.Equal(fromT.Truncate(metastore.MetastoreWindowSize)) || !throughT.Equal(fromT.Add(metastore.MetastoreWindowSize)) {
		return nil, status.Errorf(codes.InvalidArgument,
			"resolve window [%s, %s) is not a single %s UTC-aligned window", fromT, throughT, metastore.MetastoreWindowSize)
	}

	if matchers == "" {
		// The metastore requires at least one stream matcher; nothing to resolve otherwise. The
		// querier already guards this, so an empty request is defensive rather than expected.
		return &logproto.ResolveDataObjectSectionsResponse{}, nil
	}
	parsedMatchers, err := syntax.ParseMatchers(matchers, true)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parsing matchers: %v", err)
	}
	if len(parsedMatchers) == 0 {
		return &logproto.ResolveDataObjectSectionsResponse{}, nil
	}

	// GetIndexes is the cheap ToC listing; it runs per request to derive the cache key and to record
	// the exact index-object set in the cached value for collision detection.
	indexes, err := r.metastore.GetIndexes(ctx, metastore.GetIndexesRequest{Start: fromT, End: throughT})
	if err != nil {
		return nil, fmt.Errorf("listing index objects: %w", err)
	}

	// The singleflight leader shares its result with all waiters on the same key. Run the shared work
	// on a context detached from the leader's cancellation, so a leader that disconnects mid-flight
	// does not fail the waiters (whose own requests are still alive). Detaching drops the deadline, so
	// bound the work with a fixed timeout to keep a stuck object-store call from hanging the resolution
	// (and, with it, every waiter) indefinitely.
	sfKey := dataObjectSectionsCacheKey(tenant, fromT.UnixNano(), stableMatchers(matchers), stableIndexEntries(indexes.Indexes))
	v, err, _ := r.sf.Do(sfKey, func() (interface{}, error) {
		sfCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dataObjectSectionsResolveTimeout)
		defer cancel()
		if cached, ok := r.cache.get(sfCtx, tenant, from, matchers, indexes.Indexes); ok {
			r.cacheHits.Inc()
			return cached, nil
		}
		r.cacheMiss.Inc()
		sections, err := r.metastore.Sections(sfCtx, metastore.SectionsRequest{Start: fromT, End: throughT, Matchers: parsedMatchers})
		if err != nil {
			return nil, fmt.Errorf("resolving sections: %w", err)
		}
		resp := buildResolveDataObjectSectionsResponse(sections.Sections)
		r.cache.put(sfCtx, tenant, from, matchers, indexes.Indexes, resp)
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*logproto.ResolveDataObjectSectionsResponse), nil
}

// buildResolveDataObjectSectionsResponse groups the flat section descriptors by object, in a deterministic order so the
// serialized bytes are stable for caching.
func buildResolveDataObjectSectionsResponse(sections []*metastore.DataobjSectionDescriptor) *logproto.ResolveDataObjectSectionsResponse {
	byObject := make(map[string][]logproto.ResolvedDataObjectSection)
	for _, d := range sections {
		byObject[d.ObjectPath] = append(byObject[d.ObjectPath], logproto.ResolvedDataObjectSection{
			SectionIdx: d.SectionIdx,
			StreamIds:  d.StreamIDs,
		})
	}

	paths := make([]string, 0, len(byObject))
	for path := range byObject {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	objects := make([]logproto.ResolvedDataObject, 0, len(paths))
	for _, path := range paths {
		secs := byObject[path]
		sort.Slice(secs, func(i, j int) bool { return secs[i].SectionIdx < secs[j].SectionIdx })
		objects = append(objects, logproto.ResolvedDataObject{ObjectPath: path, Sections: secs})
	}
	return &logproto.ResolveDataObjectSectionsResponse{Objects: objects}
}

// resolveOutcome classifies a Resolve result for the duration metric's "outcome" label. It reports
// "canceled" only when the caller's context is done (a client cancellation or deadline), so a normal
// query cancellation does not inflate the "error" series alerts watch. Any other failure — including
// the internal singleflight guard's own timeout, which fires on a still-live caller context — is a
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
