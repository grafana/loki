package labelaccess

import (
	"context"
	"slices"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type RequestChunkFilterer struct{}

func (r *RequestChunkFilterer) ForRequest(ctx context.Context) chunk.Filterer {
	f := cachedFiltererForRequest(ctx)
	if f == nil {
		// Return nil chunk.Filterer instead of (*ChunkFilterer)(nil)
		// to avoid typed nil wrapped in interface issues.
		return nil
	}
	return f
}

var filtererCache = func() *lru.Cache[filtererCacheKey, *ChunkFilterer] {
	c, err := lru.New[filtererCacheKey, *ChunkFilterer](1024)
	if err != nil {
		panic(err)
	}
	return c
}()

type filtererCacheKey struct {
	tenant   string
	policies string
}

// cachedFiltererForRequest extracts LBAC policies from the context and returns a ChunkFilterer.
// It returns nil, which disables filtering, when the context has no policy for
// the tenant or when it cannot read the policies or the tenant.
// Avoids additional work by using a cached filterer when possible.
func cachedFiltererForRequest(ctx context.Context) *ChunkFilterer {
	headers := httpreq.ExtractAllHeaders(ctx)
	if headers == nil {
		return nil
	}
	lbacHeaderValues := headers.Values(HTTPHeaderKey)
	if len(lbacHeaderValues) == 0 {
		return nil
	}
	tenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance name", "err", err)
		return nil
	}

	// ExtractLabelMatchersFromHeaders splits every header value on commas, so joining several values
	// with a comma does not change what they parse to.
	key := filtererCacheKey{tenant: tenant, policies: strings.Join(lbacHeaderValues, ",")}
	if cf, ok := filtererCache.Get(key); ok {
		return cf
	}
	cf := filtererForRequest(ctx, tenant)
	filtererCache.Add(key, cf)
	return cf
}

func filtererForRequest(ctx context.Context, instanceName string) *ChunkFilterer {
	// Extract policies from context
	instancePolicyMap, err := ExtractLabelMatchersContext(ctx)
	if err != nil {
		if err == errNoMatcherSource {
			// No LBAC policies, return nil (no filtering)
			return nil
		}
		_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance policy map in context", "err", err)
		return nil
	}

	policies, exist := instancePolicyMap[instanceName]
	if !exist {
		return nil
	}

	// Store in ChunkFilterer
	cf := ChunkFilterer{
		policies: policies,
	}
	cf.loadMatchers()

	return &cf
}

type ChunkFilterer struct {
	policies []*types.LabelPolicy
	matchers [][]*labels.Matcher
}

// loadMatchers compiles the matchers of every policy once, so that ShouldFilter, which runs for
// every series or stream of a request, only has to evaluate them.
func (c *ChunkFilterer) loadMatchers() {
	compiled := make([][]*labels.Matcher, 0, len(c.policies))
policies:
	for _, p := range c.policies {
		matchers := make([]*labels.Matcher, 0, len(p.Selector))
		for _, lm := range p.Selector {
			matcher, err := types.LabelMatcherToPromLabel(lm)
			if err != nil {
				// This shouldn't happen: log it and do not match
				_ = level.Debug(util_log.Logger).Log("msg", "unable to encode matcher as prometheus matcher", "name", lm.Name, "err", err)
				continue policies
			}
			matchers = append(matchers, matcher)
		}
		compiled = append(compiled, matchers)
	}
	c.matchers = compiled
}

func (c *ChunkFilterer) ShouldFilter(metric labels.Labels) bool {
	if len(c.policies) == 0 {
		return false
	}

	if c.matchers == nil {
		c.loadMatchers()
	}

	for _, matchers := range c.matchers {
		// For one selector all must match
		matching := true
		for _, m := range matchers {
			if !m.Matches(metric.Get(m.Name)) {
				matching = false
				break
			}
		}
		// If any of the selectors match we should not filter
		if matching {
			return false
		}
	}
	return true
}

func (c *ChunkFilterer) PushableLabelMatchers() []*labels.Matcher {
	// Policies combine with OR, but the label matchers of one policy combine with AND. We can push
	// down label matchers only when the request has a single policy.
	if len(c.policies) != 1 {
		return nil
	}

	selector := c.policies[0].Selector
	matchers := make([]*labels.Matcher, 0, len(selector))
	for _, lm := range selector {
		matcher, err := types.LabelMatcherToPromLabel(lm)
		if err != nil {
			// ShouldFilter drops every stream for a policy it cannot convert, so push nothing and
			// let it do so.
			return nil
		}
		matchers = append(matchers, matcher)
	}
	return matchers
}

func (c *ChunkFilterer) RequiredLabelNames() []string {
	labelNames := map[string]struct{}{}
	for _, p := range c.policies {
		for _, lm := range p.Selector {
			labelNames[lm.Name] = struct{}{}
		}
	}

	names := make([]string, 0, len(labelNames))
	for k := range labelNames {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}
