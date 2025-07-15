package validation

import (
	"fmt"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	GlobalPolicy = "*"
)

type PriorityStream struct {
	Priority int               `yaml:"priority" json:"priority" doc:"description=The bigger the value, the higher the priority."`
	Selector string            `yaml:"selector" json:"selector" doc:"description=Stream selector expression."`
	Matchers []*labels.Matcher `yaml:"-" json:"-"` // populated during validation.
}

func (p *PriorityStream) Matches(lbs labels.Labels) bool {
	for _, m := range p.Matchers {
		if !m.Matches(lbs.Get(m.Name)) {
			return false
		}
	}
	return true
}

type PolicyStreamMapping map[string][]*PriorityStream

func (p *PolicyStreamMapping) Validate() error {
	for policyName, policyStreams := range *p {
		for idx, policyStream := range policyStreams {
			matchers, err := syntax.ParseMatchers(policyStream.Selector, true)
			if err != nil {
				return fmt.Errorf("invalid labels matchers for policy stream mapping: %w", err)
			}
			(*p)[policyName][idx].Matchers = matchers
		}

		// Sort the mappings by priority. Higher priority mappings come first.
		slices.SortFunc(policyStreams, func(a, b *PriorityStream) int {
			return b.Priority - a.Priority
		})
	}

	return nil
}

// PolicyFor returns all the policies that matches the given labels with the highest priority.
// Note that this method will return multiple policies if two different policies match the same labels
// with the same priority.
// Returned policies are sorted alphabetically.
// If no policies match, it returns an empty slice.
func (p *PolicyStreamMapping) PolicyFor(lbs labels.Labels) []string {
	var (
		found           bool
		highestPriority int
		matchedPolicies = make(map[string]int, len(*p))
	)

	for policyName, policyStreams := range *p {
		for _, policyStream := range policyStreams {
			// Mappings are sorted by priority (see PolicyStreamMapping.Validate at this file).
			// So we can break early if the current policy has a lower priority than the highest priority matched policy.
			if found && policyStream.Priority < highestPriority {
				break
			}

			if !policyStream.Matches(lbs) {
				continue
			}

			found = true
			highestPriority = policyStream.Priority
			matchedPolicies[policyName] = policyStream.Priority
		}
	}

	// Stick with only the highest priority policies.
	policies := make([]string, 0, len(matchedPolicies))
	for policyName, priority := range matchedPolicies {
		if priority == highestPriority {
			policies = append(policies, policyName)
		}
	}

	// Sort the policies alphabetically.
	slices.Sort(policies)

	return policies
}
