package retention

import (
	"time"

	"github.com/grafana/loki/pkg/util/validation"
	"github.com/prometheus/common/model"
)

type ExpirationChecker interface {
	Expired(ref ChunkEntry) bool
}

type expirationChecker struct {
	limits Limits
}

type Limits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
}

func NewExpirationChecker(limits Limits) ExpirationChecker {
	return &expirationChecker{
		limits: limits,
	}
}

// Expired tells if a ref chunk is expired based on retention rules.
func (e *expirationChecker) Expired(ref ChunkEntry) bool {
	userID := unsafeGetString(ref.UserID)
	streamRetentions := e.limits.StreamRetention(userID)
	globalRetention := e.limits.RetentionPeriod(userID)
	var (
		matchedRule validation.StreamRetention
		found       bool
	)
Outer:
	for _, streamRetention := range streamRetentions {
		for _, m := range streamRetention.Matchers {
			if !m.Matches(ref.Labels.Get(m.Name)) {
				continue Outer
			}
		}
		// the rule is matched.
		if found {
			// if the current matched rule has a higher priority we keep it.
			if matchedRule.Priority > streamRetention.Priority {
				continue
			}
			// if priority is equal we keep the lowest retention.
			if matchedRule.Priority == streamRetention.Priority && matchedRule.Period <= streamRetention.Period {
				continue
			}
		}
		found = true
		matchedRule = streamRetention
	}
	if found {
		return model.Now().Sub(ref.Through) > matchedRule.Period
	}
	return model.Now().Sub(ref.Through) > globalRetention
}
