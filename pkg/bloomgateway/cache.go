package bloomgateway

import (
	"context"
	"flag"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
)

const (
	cacheParalellism = 1
)

type CacheConfig struct {
	resultscache.Config `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *CacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "bloom-gateway.cache.")
}

type CacheLimits interface {
	resultscache.Limits
	BloomGatewayCacheKeyInterval(tenantID string) time.Duration
}

type keyGen struct {
	CacheLimits
}

func newCacheKeyGen(limits CacheLimits) keyGen {
	return keyGen{limits}
}

func (k keyGen) GenerateCacheKey(ctx context.Context, tenant string, r resultscache.Request) string {
	return resultscache.ConstSplitter(k.BloomGatewayCacheKeyInterval(tenant)).GenerateCacheKey(ctx, tenant, r)
}

type extractor struct{}

func newExtractor() extractor {
	return extractor{}
}

// Extract extracts a subset of a response from the `start` and `end` timestamps in milliseconds.
// We remove chunks that are not within the given time range.
func (e extractor) Extract(start, end int64, r resultscache.Response, _, _ int64) resultscache.Response {
	res := r.(*logproto.FilterChunkRefResponse)

	chunkRefs := make([]*logproto.GroupedChunkRefs, 0, len(res.ChunkRefs))
	for _, chunkRef := range res.ChunkRefs {
		refs := make([]*logproto.ShortRef, 0, len(chunkRef.Refs))
		for _, ref := range chunkRef.Refs {
			if model.Time(end) < ref.From || ref.Through <= model.Time(start) {
				continue
			}
			refs = append(refs, ref)
		}
		if len(refs) > 0 {
			chunkRefs = append(chunkRefs, &logproto.GroupedChunkRefs{
				Fingerprint: chunkRef.Fingerprint,
				Tenant:      chunkRef.Tenant,
				Refs:        refs,
			})
		}
	}

	return &logproto.FilterChunkRefResponse{
		ChunkRefs: chunkRefs,
	}
}

type merger struct{}

func newMerger() merger {
	return merger{}
}

// MergeResponse merges responses from multiple requests into a single Response
// We merge all chunks grouped by their fingerprint.
func (m merger) MergeResponse(responses ...resultscache.Response) (resultscache.Response, error) {
	var size int
	for _, r := range responses {
		res := r.(*logproto.FilterChunkRefResponse)
		size += len(res.ChunkRefs)
	}

	chunkRefs := make([]*logproto.GroupedChunkRefs, 0, size)
	for _, r := range responses {
		res := r.(*logproto.FilterChunkRefResponse)
		chunkRefs = append(chunkRefs, res.ChunkRefs...)
	}

	return &logproto.FilterChunkRefResponse{
		ChunkRefs: mergeGroupedChunkRefs(chunkRefs),
	}, nil
}

// Merge duplicated fingerprints by:
// 1. Sort the chunkRefs by their stream fingerprint
// 2. Append all chunks with the same fingerprint into the first fingerprint's chunk list.
func mergeGroupedChunkRefs(chunkRefs []*logproto.GroupedChunkRefs) []*logproto.GroupedChunkRefs {
	sort.Slice(chunkRefs, func(i, j int) bool {
		return chunkRefs[i].Fingerprint < chunkRefs[j].Fingerprint
	})
	return slices.CompactFunc(chunkRefs, func(next, prev *logproto.GroupedChunkRefs) bool {
		if next.Fingerprint == prev.Fingerprint {
			prev.Refs = mergeShortRefs(append(prev.Refs, next.Refs...))
			return true
		}
		return false
	})
}

// mergeShortRefs merges short-refs by removing duplicated checksums.
func mergeShortRefs(refs []*logproto.ShortRef) []*logproto.ShortRef {
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Checksum < refs[j].Checksum
	})
	return slices.CompactFunc(refs, func(a, b *logproto.ShortRef) bool {
		return a.Checksum == b.Checksum
	})
}

type ClientCache struct {
	cache  *resultscache.ResultsCache
	limits CacheLimits
	logger log.Logger
}

func NewBloomGatewayClientCacheMiddleware(
	logger log.Logger,
	next logproto.BloomGatewayClient,
	c cache.Cache,
	limits CacheLimits,
	cacheGen resultscache.CacheGenNumberLoader,
	retentionEnabled bool,
) *ClientCache {
	nextAsHandler := resultscache.HandlerFunc(func(ctx context.Context, req resultscache.Request) (resultscache.Response, error) {
		return next.FilterChunkRefs(ctx, req.(*logproto.FilterChunkRefRequest))
	})

	resultsCache := resultscache.NewResultsCache(
		logger,
		c,
		nextAsHandler,
		newCacheKeyGen(limits),
		limits,
		newMerger(),
		newExtractor(),
		nil,
		nil,
		func(_ context.Context, _ []string, _ resultscache.Request) int {
			return cacheParalellism
		},
		cacheGen,
		retentionEnabled,
	)

	return &ClientCache{
		cache:  resultsCache,
		limits: limits,
		logger: logger,
	}
}

func (c *ClientCache) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest, _ ...grpc.CallOption) (*logproto.FilterChunkRefResponse, error) {
	res, err := c.cache.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	return res.(*logproto.FilterChunkRefResponse), nil
}
