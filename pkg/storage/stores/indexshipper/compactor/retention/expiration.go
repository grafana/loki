package retention

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/util/filter"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

// IntervalFilter contains the interval to delete
// and the function that filters lines. These will be
// applied to a chunk.
type IntervalFilter struct {
	Interval model.Interval
	Filter   filter.Func
}

type ExpirationChecker interface {
	Expired(ref ChunkEntry, now model.Time) (bool, filter.Func)
	IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool
	MarkPhaseStarted()
	MarkPhaseFailed()
	MarkPhaseTimedOut()
	MarkPhaseFinished()
	DropFromIndex(ref ChunkEntry, tableEndTime model.Time, now model.Time) bool
}

type expirationChecker struct {
	tenantsRetention         *TenantsRetention
	latestRetentionStartTime latestRetentionStartTime
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
func (e *expirationChecker) Expired(ref ChunkEntry, now model.Time) (bool, filter.Func) {
	userID := unsafeGetString(ref.UserID)
	period := e.tenantsRetention.RetentionPeriodFor(userID, ref.Labels)
	// The 0 value should disable retention
	if period <= 0 {
		return false, nil
	}
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
	e.latestRetentionStartTime = findLatestRetentionStartTime(model.Now(), e.tenantsRetention.limits)
	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("overall smallest retention period %v, default smallest retention period %v",
		e.latestRetentionStartTime.overall, e.latestRetentionStartTime.defaults))
}

func (e *expirationChecker) MarkPhaseFailed()   {}
func (e *expirationChecker) MarkPhaseTimedOut() {}
func (e *expirationChecker) MarkPhaseFinished() {}

func (e *expirationChecker) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	// when userID is empty, it means we are checking for common index table. In this case we use e.overallLatestRetentionStartTime.
	latestRetentionStartTime := e.latestRetentionStartTime.overall
	if userID != "" {
		// when userID is not empty, it means we are checking for user index table.
		latestRetentionStartTimeForUser, ok := e.latestRetentionStartTime.byUser[userID]
		if ok {
			// user has custom retention config, let us use user specific latest retention start time.
			latestRetentionStartTime = latestRetentionStartTimeForUser
		} else {
			// user does not have custom retention config, let us use default latest retention start time.
			latestRetentionStartTime = e.latestRetentionStartTime.defaults
		}
	}
	return interval.Start.Before(latestRetentionStartTime)
}

// NeverExpiringExpirationChecker returns an expiration checker that never expires anything
func NeverExpiringExpirationChecker(limits Limits) ExpirationChecker {
	return &neverExpiringExpirationChecker{}
}

type neverExpiringExpirationChecker struct{}

func (e *neverExpiringExpirationChecker) Expired(ref ChunkEntry, now model.Time) (bool, filter.Func) {
	return false, nil
}
func (e *neverExpiringExpirationChecker) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	return false
}
func (e *neverExpiringExpirationChecker) MarkPhaseStarted()  {}
func (e *neverExpiringExpirationChecker) MarkPhaseFailed()   {}
func (e *neverExpiringExpirationChecker) MarkPhaseTimedOut() {}
func (e *neverExpiringExpirationChecker) MarkPhaseFinished() {}
func (e *neverExpiringExpirationChecker) DropFromIndex(ref ChunkEntry, tableEndTime model.Time, now model.Time) bool {
	return false
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

type latestRetentionStartTime struct {
	// defaults holds latest retention start time considering only default retention config.
	// It is used to determine if user index table may have any expired chunks when the user does not have any custom retention config set.
	defaults model.Time
	// overall holds latest retention start time for all users considering both default and per user retention config.
	// It is used to determine if common index table may have any expired chunks.
	overall model.Time
	// byUser holds latest retention start time considering only per user retention config.
	// It is used to determine if user index table may have any expired chunks.
	byUser map[string]model.Time
}

// findLatestRetentionStartTime returns the latest retention start time overall, just default config and by each user.
func findLatestRetentionStartTime(now model.Time, limits Limits) latestRetentionStartTime {
	// find the smallest retention period from default limits
	defaultLimits := limits.DefaultLimits()
	smallestDefaultRetentionPeriod := defaultLimits.RetentionPeriod
	for _, streamRetention := range defaultLimits.StreamRetention {
		if streamRetention.Period < smallestDefaultRetentionPeriod {
			smallestDefaultRetentionPeriod = streamRetention.Period
		}
	}

	overallSmallestRetentionPeriod := smallestDefaultRetentionPeriod

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

		// update the overallSmallestRetentionPeriod if this user has smaller value
		smallestRetentionPeriodByUser[userID] = now.Add(time.Duration(-smallestRetentionPeriodForUser))
		if smallestRetentionPeriodForUser < overallSmallestRetentionPeriod {
			overallSmallestRetentionPeriod = smallestRetentionPeriodForUser
		}
	}

	return latestRetentionStartTime{
		defaults: now.Add(time.Duration(-smallestDefaultRetentionPeriod)),
		overall:  now.Add(time.Duration(-overallSmallestRetentionPeriod)),
		byUser:   smallestRetentionPeriodByUser,
	}
}
