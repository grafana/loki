package querier

import (
	"github.com/go-kit/log"
)

// MultiTenantQuerier is able to query across different tenants.
type MultiTenantQuerier struct {
	SingleTenantQuerier
}

// NewMultiTenantQuerier returns a new querier able to query across different tenants.
func NewMultiTenantQuerier(querier SingleTenantQuerier, logger log.Logger) *MultiTenantQuerier {
	return &MultiTenantQuerier{SingleTenantQuerier: querier}
}
