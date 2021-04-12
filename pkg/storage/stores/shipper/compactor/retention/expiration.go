package retention

import (
	"github.com/prometheus/common/model"
)

type ExpirationChecker interface {
	Expired(ref *ChunkRef) bool
}

// func MultiExpirationCheck(checker ...ExpirationChecker) ExpirationChecker {

// }

type expirationChecker struct {
	series map[string]StreamRule
	TenantRules
}

func NewExpirationChecker(seriesPerRule map[string]StreamRule, rules TenantRules) ExpirationChecker {
	return &expirationChecker{
		series:      seriesPerRule,
		TenantRules: rules,
	}
}

// Expired tells if a ref chunk is expired based on retention rules.
func (e *expirationChecker) Expired(ref *ChunkRef) bool {
	r, ok := e.series[string(ref.SeriesID)]
	// if the series matches a stream rules we'll use that.
	if ok && r.UserID == string(ref.UserID) {
		return ref.From.After(model.Now().Add(r.Duration))
	}
	return ref.From.After(model.Now().Add(e.TenantRules.PerTenant(unsafeGetString(ref.UserID))))
}

// all chunk ref.... chunkid,seriesid, from,to, userid (labels) (n + 1)
//
// HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey, metricName, v.Name),
// RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
// Value:      []byte(v.Value),

// custom stream only {app="foo"}
