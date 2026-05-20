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
// If the orgID contains metadata (format "tenantID:key=value"), this function
// strips the metadata part and returns only the tenant ID. This ensures backward
// compatibility with existing code that is not metadata-aware.
//
// The metadata is not validated.
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
	tenantID := TrimMetadata(orgID)
	if err := ValidTenantID(tenantID); err != nil {
		return "", err
	}
	for hasMoreIDs {
		orgID, remaining, hasMoreIDs = stringsCut(remaining, tenantIDsSeparator)
		if tenantID != TrimMetadata(orgID) {
			return "", user.ErrTooManyOrgIDs
		}
	}
	return tenantID, nil
}

// TenantIDs returns all tenant IDs from the context. It should return
// normalized list of ordered and distinct tenant IDs (as produced by
// NormalizeTenantIDs).
//
// If the orgID contains metadata (format "tenantID:key=value" or
// "tenant1:k=v1|tenant2:k=v2"), this function strips the metadata parts
// and returns only the tenant IDs. This ensures backward compatibility
// with existing code that is not metadata-aware.
//
// Metadata is not validated.
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
		tenantId := TrimMetadata(part)
		if err := ValidTenantID(tenantId); err != nil {
			return nil, err
		}
		orgIDs[i] = tenantId
	}
	return NormalizeTenantIDs(orgIDs), nil
}

// ExtractWithMetadata returns the tenant ID and optional metadata from the context,
// or nil metadata if no metadata is present. The orgID format is "tenantID:metadata"
// (e.g., "123456:test=yes").
//
//nolint:revive
func ExtractWithMetadata(ctx context.Context) (tenantID string, m Metadata, err error) {
	//lint:ignore faillint wrapper around upstream method
	orgIDs, err := user.ExtractOrgID(ctx)
	if err != nil {
		return "", Metadata{}, err
	}
	return ParseWithMetadata(orgIDs)
}

// ParseWithMetadata returns the tenant ID and optional metadata from orgID(s).
// Metadata is nil if no metadata is present. The orgID format is
// "tenantID:key=value" or "tenantID:k1=v1:k2=v2".
//
// Examples:
//   - 123456              (tenantID=123456, metadata=nil)
//   - 123456:a=b          (tenantID=123456, metadata={a: b})
//   - 123456:a=b:c=d      (tenantID=123456, metadata={a: b, c: d})
func ParseWithMetadata(orgIDs string) (tenantID string, m Metadata, err error) {
	orgID, remaining, hasMoreIDs := stringsCut(orgIDs, tenantIDsSeparator)
	tenantID, metadataString := splitTenantAndMetadata(orgID)
	if err := ValidTenantID(tenantID); err != nil {
		return "", Metadata{}, err
	}
	if err := ValidMetadata(metadataString); err != nil {
		return "", Metadata{}, err
	}
	var nextOrgID string
	for hasMoreIDs {
		nextOrgID, remaining, hasMoreIDs = stringsCut(remaining, tenantIDsSeparator)
		// We can compare the entire orgID, no need to split into tenant/metadata.
		// The orgID is already guaranteed to be valid.
		if orgID != nextOrgID {
			return "", Metadata{}, user.ErrTooManyOrgIDs
		}
	}
	m, err = ParseMetadata(metadataString)
	if err != nil {
		return "", Metadata{}, err
	}
	return tenantID, m, nil
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
