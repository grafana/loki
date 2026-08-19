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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/dataobj/dataobjmetrics"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// DataObjectSectionsResolver resolves the data-object sections matching a query within a single 12h
// UTC-aligned window. It is the server-side implementation behind the
// IndexGateway.ResolveDataObjectSections RPC.
//
// It is a thin adapter over metastore.Sections: the metastore lists the window's index objects, caches
// the resolution, and collapses concurrent identical resolutions with a singleflight. The resolver only
// validates the window, parses the matchers, and shapes the result into the RPC wire type.
type DataObjectSectionsResolver struct {
	metastore metastore.Metastore
	logger    log.Logger

	// dataObjMetrics folds this resolve's object-store reads into the index-gateway's per-component
	// request/byte counters. The metastore records reads against the xcap Capture Resolve installs.
	dataObjMetrics *dataobjmetrics.Metrics
	duration       *prometheus.HistogramVec
}

// NewDataObjectSectionsResolver builds a resolver over ms. The section cache and singleflight live in
// the metastore, so the resolver takes no cache.
func NewDataObjectSectionsResolver(ms metastore.Metastore, reg prometheus.Registerer, logger log.Logger) *DataObjectSectionsResolver {
	return &DataObjectSectionsResolver{
		metastore:      ms,
		logger:         logger,
		dataObjMetrics: dataobjmetrics.New(prometheus.WrapRegistererWithPrefix("loki_index_gateway_", reg)),
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
// single MetastoreWindowSize (12h) UTC-aligned window; an unaligned window is a client bug and returns
// codes.InvalidArgument.
func (r *DataObjectSectionsResolver) Resolve(ctx context.Context, from, through model.Time, matchers string) (resp *logproto.ResolveDataObjectSectionsResponse, err error) {
	// One capture spans the whole resolve so every object-storage read the metastore issues is folded
	// into the index-gateway's per-component request and byte metrics.
	ctx, capture := xcap.NewCapture(ctx, nil)
	defer func(start time.Time) {
		capture.End()
		r.dataObjMetrics.Record(capture)
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

	sections, err := r.metastore.Sections(ctx, metastore.SectionsRequest{Start: fromT, End: throughT, Matchers: parsedMatchers})
	if err != nil {
		return nil, fmt.Errorf("resolving sections: %w", err)
	}
	return buildResolveDataObjectSectionsResponse(sections.Sections), nil
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
// the metastore's internal singleflight guard timeout, which fires on a still-live caller context — is
// a real "error".
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
