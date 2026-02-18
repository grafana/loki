package labelaccess

import (
	"context"
	"slices"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/prometheus/prometheus/model/labels"
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
// This is shared between RequestChunkFilterer (V1 engine) and RequestStreamFilterer (V2 engine).
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

	// Store in ChunkFilterer
	cf := ChunkFilterer{
		policies: policies,
	}
	cf.loadMatchers()

	return &cf
}

type ChunkFilterer struct {
	policies []*types.LabelPolicy
	matchers map[string]*labels.Matcher
}

func (c *ChunkFilterer) loadMatchers() {
	matchers := make(map[string]*labels.Matcher)
	for _, p := range c.policies {
		for _, lm := range p.Selector {
			matcher, err := types.LabelMatcherToPromLabel(lm)
			if err != nil {
				// This shouldn't happen: log it and do not match
				_ = level.Debug(util_log.Logger).Log("msg", "unable to encode matcher as prometheus matcher", "err", err)
				break
			}

			matchers[lm.GoString()] = matcher
		}
	}
	c.matchers = matchers
}

func (c *ChunkFilterer) ShouldFilter(metric labels.Labels) bool {
	if len(c.policies) == 0 {
		return false
	}

	if len(c.matchers) == 0 {
		c.loadMatchers()
	}

	for _, p := range c.policies {
		// For one selector all must match
		matching := true
		for _, lm := range p.Selector {
			labelValue := metric.Get(lm.Name)

			matcher, ok := c.matchers[lm.GoString()]
			if !ok {
				// This shouldn't happen: log it and do not match
				_ = level.Debug(util_log.Logger).Log("msg", "unable to find matcher as prometheus matcher", "name", lm.Name)
				matching = false
				break
			}

			if !matcher.Matches(labelValue) {
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
