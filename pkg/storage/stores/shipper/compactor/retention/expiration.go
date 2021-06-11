package retention

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/validation"
)

type ExpirationChecker interface {
	Expired(ref ChunkEntry, now model.Time) (bool, []model.Interval)
}

type expirationChecker struct {
	tenantsRetention *TenantsRetention
}

type Limits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
}

func NewExpirationChecker(limits Limits) ExpirationChecker {
	return &expirationChecker{
		tenantsRetention: NewTenantsRetention(limits),
	}
}

// Expired tells if a ref chunk is expired based on retention rules.
func (e *expirationChecker) Expired(ref ChunkEntry, now model.Time) (bool, []model.Interval) {
	userID := unsafeGetString(ref.UserID)
	period := e.tenantsRetention.RetentionPeriodFor(userID, ref.Labels)
	return now.Sub(ref.Through) > period, nil
}

type TenantsRetention struct {
	limits Limits
}

func NewTenantsRetention(l Limits) *TenantsRetention {
	return &TenantsRetention{
		limits: l,
	}
}

func (tr *TenantsRetention) RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration {
	streamRetentions := tr.limits.StreamRetention(userID)
	globalRetention := tr.limits.RetentionPeriod(userID)
	var (
		matchedRule validation.StreamRetention
		found       bool
	)
Outer:
	for _, streamRetention := range streamRetentions {
		for _, m := range streamRetention.Matchers {
			if !m.Matches(lbs.Get(m.Name)) {
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
		return time.Duration(matchedRule.Period)
	}
	return globalRetention
}
