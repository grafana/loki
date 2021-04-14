package retention

import (
	"github.com/prometheus/common/model"
)

type ExpirationChecker interface {
	Expired(ref ChunkEntry) bool
}

type expirationChecker struct {
	rules Rules
}

func NewExpirationChecker(rules Rules) ExpirationChecker {
	return &expirationChecker{
		rules: rules,
	}
}

// Expired tells if a ref chunk is expired based on retention rules.
func (e *expirationChecker) Expired(ref ChunkEntry) bool {
	// if the series matches a stream rules we'll use that.
	if ok && r.UserID == string(ref.UserID) {
		return ref.From.After(model.Now().Add(r.Duration))
	}
	return ref.From.After(model.Now().Add(e.TenantRules.PerTenant(unsafeGetString(ref.UserID))))
}
