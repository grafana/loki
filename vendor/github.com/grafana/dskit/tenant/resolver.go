package tenant

import (
	"context"
	"strings"

	"github.com/grafana/dskit/user"
)

// TenantID returns exactly a single tenant ID from the context. It should be
// used when a certain endpoint should only support exactly a single
// tenant ID. It returns an error user.ErrNoOrgID if there is no tenant ID
// supplied or user.ErrTooManyOrgIDs if there are multiple tenant IDs present.
//
// If the orgID contains a subtenant (format "tenantID:subtenantID"), this function
// strips the subtenant part and returns only the tenant ID. This ensures backward
// compatibility with existing code that is not subtenant-aware.
//
// The SubtenantID is not validated.
//
// ignore stutter warning
//
//nolint:revive
func TenantID(ctx context.Context) (string, error) {
	//lint:ignore faillint wrapper around upstream method
	orgIDs, err := user.ExtractOrgID(ctx)
	if err != nil {
		return "", err
	}
	orgID, remaining, hasMoreIDs := stringsCut(orgIDs, tenantIDsSeparator)
	tenantID := trimSubtenantID(orgID)
	if err := ValidTenantID(tenantID); err != nil {
		return "", err
	}
	for hasMoreIDs {
		orgID, remaining, hasMoreIDs = stringsCut(remaining, tenantIDsSeparator)
		if tenantID != trimSubtenantID(orgID) {
			return "", user.ErrTooManyOrgIDs
		}
	}
	return tenantID, nil
}

// TenantIDs returns all tenant IDs from the context. It should return
// normalized list of ordered and distinct tenant IDs (as produced by
// NormalizeTenantIDs).
//
// If the orgID contains subtenants (format "tenantID:subtenantID" or
// "tenant1:sub1|tenant2:sub2"), this function strips the subtenant parts
// and returns only the tenant IDs. This ensures backward compatibility
// with existing code that is not subtenant-aware.
//
// SubtenantIDs are not validated.
//
// ignore stutter warning
//
//nolint:revive
func TenantIDs(ctx context.Context) ([]string, error) {
	//lint:ignore faillint wrapper around upstream method
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	return parseTenantIDs(orgID)
}

func parseTenantIDs(orgID string) ([]string, error) {
	orgIDs := strings.Split(orgID, string(tenantIDsSeparator))
	for i, part := range orgIDs {
		tenantId := trimSubtenantID(part)
		if err := ValidTenantID(tenantId); err != nil {
			return nil, err
		}
		orgIDs[i] = tenantId
	}
	return NormalizeTenantIDs(orgIDs), nil
}

// SubtenantID returns the subtenant ID from the context, or an empty string if
// no subtenant is present. The orgID format is "tenantID:subtenantID" (e.g., "123456:k6").
//
//nolint:revive
func SubtenantID(ctx context.Context) (tenantID string, subtenantID string, _ error) {
	//lint:ignore faillint wrapper around upstream method
	orgIDs, err := user.ExtractOrgID(ctx)
	if err != nil {
		return "", "", err
	}

	orgID, remaining, hasMoreIDs := stringsCut(orgIDs, tenantIDsSeparator)
	tenantID, subtenantID = splitTenantAndSubtenant(orgID)
	if err := ValidTenantID(tenantID); err != nil {
		return "", "", err
	}
	if err := ValidSubtenantID(subtenantID); err != nil {
		return "", "", err
	}
	var nextOrgID string
	for hasMoreIDs {
		nextOrgID, remaining, hasMoreIDs = stringsCut(remaining, tenantIDsSeparator)
		// We can compare the entire orgID, no need to split into tenant/subtenant.
		// The orgID is already guaranteed to be valid.
		if orgID != nextOrgID {
			return "", "", user.ErrTooManyOrgIDs
		}
	}
	return tenantID, subtenantID, nil
}

// SubtenantIDs returns a normalized list of all subtenant IDs from the context
// (as produced by NormalizeTenantIDs). Empty subtenants are omitted from the
// result.
//
// ignore stutter warning
//
//nolint:revive
func SubtenantIDs(ctx context.Context) ([]string, error) {
	//lint:ignore faillint wrapper around upstream method
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	return parseSubtenantIDs(orgID)
}

func parseSubtenantIDs(orgID string) ([]string, error) {
	parts := strings.Split(orgID, string(tenantIDsSeparator))
	var subtenantIDs []string
	for _, part := range parts {
		tenantID, subtenantID := splitTenantAndSubtenant(part)
		if err := ValidTenantID(tenantID); err != nil {
			return nil, err
		}
		if subtenantID != "" {
			if err := ValidSubtenantID(subtenantID); err != nil {
				return nil, err
			}
			subtenantIDs = append(subtenantIDs, subtenantID)
		}
	}
	return NormalizeTenantIDs(subtenantIDs), nil
}

type Resolver interface {
	// TenantID returns exactly a single tenant ID from the context. It should be
	// used when a certain endpoint should only support exactly a single
	// tenant ID. It returns an error user.ErrNoOrgID if there is no tenant ID
	// supplied or user.ErrTooManyOrgIDs if there are multiple tenant IDs present.
	TenantID(context.Context) (string, error)

	// TenantIDs returns all tenant IDs from the context. It should return
	// normalized list of ordered and distinct tenant IDs (as produced by
	// NormalizeTenantIDs).
	TenantIDs(context.Context) ([]string, error)

	// SubtenantID returns the tenant ID and subtenant ID from the context.
	// Returns empty subtenant if no subtenant is present.
	// The orgID format is "tenantID:subtenantID".
	SubtenantID(context.Context) (string, string, error)

	// SubtenantIDs returns all subtenant IDs from the context. It should return
	// a normalized list of ordered and distinct subtenant IDs.
	SubtenantIDs(context.Context) ([]string, error)
}

type MultiResolver struct{}

var _ Resolver = NewMultiResolver()

// NewMultiResolver creates a tenant resolver, which allows request to have
// multiple tenant ids submitted separated by a '|' character. This enforces
// further limits on the character set allowed within tenants as detailed here:
// https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/
func NewMultiResolver() *MultiResolver {
	return &MultiResolver{}
}

func (t *MultiResolver) TenantID(ctx context.Context) (string, error) {
	return TenantID(ctx)
}

func (t *MultiResolver) TenantIDs(ctx context.Context) ([]string, error) {
	return TenantIDs(ctx)
}

func (t *MultiResolver) SubtenantID(ctx context.Context) (string, string, error) {
	return SubtenantID(ctx)
}

func (t *MultiResolver) SubtenantIDs(ctx context.Context) ([]string, error) {
	return SubtenantIDs(ctx)
}
