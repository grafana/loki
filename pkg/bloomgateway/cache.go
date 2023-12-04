package bloomgateway

import (
	"context"
	"flag"
	"sort"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
)

type CacheConfig struct {
	resultscache.Config `yaml:",inline"`
	Parallelism         int `yaml:"parallelism"`
}

// RegisterFlags registers flags.
func (cfg *CacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "bloom-gateway.cache.")
	f.IntVar(&cfg.Parallelism, "bloom-gateway.cache.parallelism", 1, "Maximum number fo concurrent requests to gateway server.")
}

type keyGen struct {
	Limits
}

func NewCacheKeyGen(limits Limits) resultscache.KeyGenerator {
	return keyGen{limits}
}

func (k keyGen) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	return resultscache.ConstSplitter(k.BloomGatewayCacheKeyInterval(userID)).GenerateCacheKey(ctx, userID, r)
}

type extractor struct{}

func NewExtractor() resultscache.Extractor {
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

func NewMerger() resultscache.ResponseMerger {
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

type bloomQuerierCache struct {
	cache  *resultscache.ResultsCache
	limits Limits
	logger log.Logger
}

func NewBloomGatewayClientCacheMiddleware(
	cfg CacheConfig,
	logger log.Logger,
	next logproto.BloomGatewayClient,
	c cache.Cache,
	limits Limits,
	cacheGen resultscache.CacheGenNumberLoader,
	retentionEnabled bool,
) logproto.BloomGatewayClient {
	nextAsHandler := resultscache.HandlerFunc(func(ctx context.Context, req resultscache.Request) (resultscache.Response, error) {
		return next.FilterChunkRefs(ctx, req.(*logproto.FilterChunkRefRequest))
	})

	resultsCache := resultscache.NewResultsCache(
		logger,
		c,
		nextAsHandler,
		NewCacheKeyGen(limits),
		limits,
		NewMerger(),
		NewExtractor(),
		nil,
		nil,
		func(_ context.Context, _ []string, _ resultscache.Request) int {
			return cfg.Parallelism
		},
		cacheGen,
		retentionEnabled,
	)

	return &bloomQuerierCache{
		cache:  resultsCache,
		limits: limits,
		logger: logger,
	}
}

func (c *bloomQuerierCache) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest, _ ...grpc.CallOption) (*logproto.FilterChunkRefResponse, error) {
	res, err := c.cache.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	return res.(*logproto.FilterChunkRefResponse), nil
}
