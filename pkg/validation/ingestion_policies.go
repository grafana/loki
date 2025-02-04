package validation

import "github.com/prometheus/prometheus/model/labels"

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

func (p *PolicyStreamMapping) PolicyFor(lbs labels.Labels) string {
	var (
		matchedPolicy     *PriorityStream
		found             bool
		matchedPolicyName string
	)

	for policyName, policyStreams := range *p {
		for _, policyStream := range policyStreams {
			if found && policyStream.Priority <= matchedPolicy.Priority {
				// Even if a match occurs it won't have a higher priority than the current matched policy.
				continue
			}

			if !policyStream.Matches(lbs) {
				continue
			}

			found = true
			matchedPolicy = policyStream
			matchedPolicyName = policyName
		}
	}

	return matchedPolicyName
}
