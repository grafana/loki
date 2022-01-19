package retention

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

type ExpirationChecker interface {
	Expired(ref ChunkEntry, now model.Time) (bool, []model.Interval)
	IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool
	MarkPhaseStarted()
	MarkPhaseFailed()
	MarkPhaseFinished()
	DropFromIndex(ref ChunkEntry, tableEndTime model.Time, now model.Time) bool
}

type expirationChecker struct {
	tenantsRetention               *TenantsRetention
	latestRetentionStartTime       model.Time
	latestRetentionStartTimeByUser map[string]model.Time
}

type Limits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
	AllByUserID() map[string]*validation.Limits
	DefaultLimits() *validation.Limits
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

// DropFromIndex tells if it is okay to drop the chunk entry from index table.
// We check if tableEndTime is out of retention period, calculated using the labels from the chunk.
// If the tableEndTime is out of retention then we can drop the chunk entry without removing the chunk from the store.
func (e *expirationChecker) DropFromIndex(ref ChunkEntry, tableEndTime model.Time, now model.Time) bool {
	userID := unsafeGetString(ref.UserID)
	period := e.tenantsRetention.RetentionPeriodFor(userID, ref.Labels)
	return now.Sub(tableEndTime) > period
}

func (e *expirationChecker) MarkPhaseStarted() {
	e.latestRetentionStartTime, e.latestRetentionStartTimeByUser = findLatestRetentionStartTime(model.Now(), e.tenantsRetention.limits)
	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("smallest retention period %v", e.latestRetentionStartTime))
}

func (e *expirationChecker) MarkPhaseFailed()   {}
func (e *expirationChecker) MarkPhaseFinished() {}

func (e *expirationChecker) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	latestRetentionStartTime := e.latestRetentionStartTime
	if userID != "" {
		latestRetentionStartTimeForUser, ok := e.latestRetentionStartTimeByUser[userID]
		if ok {
			latestRetentionStartTime = latestRetentionStartTimeForUser
		}
	}
	return interval.Start.Before(latestRetentionStartTime)
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

// findLatestRetentionStartTime returns the latest retention start time overall and by each user.
func findLatestRetentionStartTime(now model.Time, limits Limits) (model.Time, map[string]model.Time) {
	// find the smallest retention period from default limits
	defaultLimits := limits.DefaultLimits()
	smallestRetentionPeriod := defaultLimits.RetentionPeriod
	for _, streamRetention := range defaultLimits.StreamRetention {
		if streamRetention.Period < smallestRetentionPeriod {
			smallestRetentionPeriod = streamRetention.Period
		}
	}

	// find the smallest retention period by user
	limitsByUserID := limits.AllByUserID()
	smallestRetentionPeriodByUser := make(map[string]model.Time, len(limitsByUserID))
	for userID, limit := range limitsByUserID {
		smallestRetentionPeriodForUser := limit.RetentionPeriod
		for _, streamRetention := range limit.StreamRetention {
			if streamRetention.Period < smallestRetentionPeriodForUser {
				smallestRetentionPeriodForUser = streamRetention.Period
			}
		}

		// update the common smallestRetentionPeriod if this user has smaller value
		smallestRetentionPeriodByUser[userID] = now.Add(time.Duration(-smallestRetentionPeriodForUser))
		if smallestRetentionPeriodForUser < smallestRetentionPeriod {
			smallestRetentionPeriod = smallestRetentionPeriodForUser
		}
	}

	return now.Add(time.Duration(-smallestRetentionPeriod)), smallestRetentionPeriodByUser
}
