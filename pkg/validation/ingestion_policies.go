package validation

import (
	"slices"

	"github.com/prometheus/prometheus/model/labels"
)

type PriorityStream struct {
	Priority int               `yaml:"priority" json:"priority" doc:"description=The larger the value, the higher the priority."`
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
			if found && policyStream.Priority < highestPriority {
				// Even if a match occurs it won't have a higher priority than the current matched policy.
				continue
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
