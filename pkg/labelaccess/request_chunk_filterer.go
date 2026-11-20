package labelaccess

import (
	"context"
	"slices"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type RequestChunkFilterer struct{}

func (r *RequestChunkFilterer) ForRequest(ctx context.Context) chunk.Filterer {
	f := filtererForRequest(ctx)
	if f == nil {
		// Return nil chunk.Filterer instead of (*ChunkFilterer)(nil)
		// to avoid typed nil wrapped in interface issues.
		return nil
	}
	return f
}

// filtererForRequest extracts LBAC policies from the context and returns a ChunkFilterer.
// It returns nil, which disables filtering, when the context has no policy for
// the tenant or when it cannot read the policies or the tenant.
func filtererForRequest(ctx context.Context) *ChunkFilterer {
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

	instanceName, err := user.ExtractOrgID(ctx)
	if err != nil {
		_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance name", "err", err)
		return nil
	}

	policies, exist := instancePolicyMap[instanceName]
	if !exist {
		return nil
	}

	return &ChunkFilterer{
		policies: policies,
	}
}

type ChunkFilterer struct {
	policies []*types.LabelPolicy
}

func (c *ChunkFilterer) ShouldFilter(metric labels.Labels) bool {
	if len(c.policies) == 0 {
		return false
	}

	for _, p := range c.policies {
		// For one selector all must match
		matching := true
		for _, m := range p.Selector {
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

	return c.policies[0].Selector
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
