package retention

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/validation"
)

type ExpirationChecker interface {
	Expired(ref ChunkEntry, now model.Time) (bool, []model.Interval)
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
func (e *expirationChecker) Expired(ref ChunkEntry, now model.Time) (bool, []model.Interval) {
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
		return now.Sub(ref.Through) > time.Duration(matchedRule.Period), nil
	}
	return now.Sub(ref.Through) > globalRetention, nil
}
