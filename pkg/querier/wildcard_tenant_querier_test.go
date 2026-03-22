package querier

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTenantDiscovery implements TenantDiscovery for testing
type mockTenantDiscovery struct {
	tenants []string
	err     error
}

func (m *mockTenantDiscovery) GetAllTenants(ctx context.Context) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tenants, nil
}

func (m *mockTenantDiscovery) IsWildcard(orgID string) bool {
	return orgID == WildcardTenantID
}

func TestWildcardTenantQuerier_resolveWildcardTenants(t *testing.T) {
	logger := log.NewNopLogger()

	tests := []struct {
		name            string
		orgID           string
		allTenants      []string
		expectedTenants []string
		expectedError   error
	}{
		{
			name:            "no wildcard - single tenant",
			orgID:           "tenant1",
			allTenants:      []string{"tenant1", "tenant2", "tenant3"},
			expectedTenants: []string{"tenant1"},
			expectedError:   nil,
		},
		{
			name:            "no wildcard - multiple tenants",
			orgID:           "tenant1|tenant2",
			allTenants:      []string{"tenant1", "tenant2", "tenant3"},
			expectedTenants: []string{"tenant1", "tenant2"},
			expectedError:   nil,
		},
		{
			name:            "wildcard only - all tenants",
			orgID:           "*",
			allTenants:      []string{"tenant1", "tenant2", "tenant3"},
			expectedTenants: []string{"tenant1", "tenant2", "tenant3"},
			expectedError:   nil,
		},
		{
			name:            "wildcard with one exclusion",
			orgID:           "*|!tenant2",
			allTenants:      []string{"tenant1", "tenant2", "tenant3"},
			expectedTenants: []string{"tenant1", "tenant3"},
			expectedError:   nil,
		},
		{
			name:            "wildcard with multiple exclusions",
			orgID:           "*|!tenant1|!tenant3",
			allTenants:      []string{"tenant1", "tenant2", "tenant3", "tenant4"},
			expectedTenants: []string{"tenant2", "tenant4"},
			expectedError:   nil,
		},
		{
			name:            "exclusion without wildcard - error",
			orgID:           "tenant1|!tenant2",
			allTenants:      []string{"tenant1", "tenant2", "tenant3"},
			expectedTenants: nil,
			expectedError:   ErrExcludeWithoutWildcard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discovery := &mockTenantDiscovery{
				tenants: tt.allTenants,
			}

			querier := &WildcardTenantQuerier{
				discovery: discovery,
				logger:    logger,
			}

			ctx := user.InjectOrgID(context.Background(), tt.orgID)
			resolvedCtx, err := querier.resolveWildcardTenants(ctx)

			if tt.expectedError != nil {
				assert.ErrorIs(t, err, tt.expectedError)
				return
			}

			require.NoError(t, err)

			// Extract org ID from the resolved context
			resolvedOrgID, err := user.ExtractOrgID(resolvedCtx)
			require.NoError(t, err)

			// For non-wildcard cases, the original context should be returned unchanged
			if tt.orgID != "*" && !containsWildcard(tt.orgID) {
				assert.Equal(t, tt.orgID, resolvedOrgID)
			} else {
				// For wildcard cases, verify the resolved tenants
				// The resolved OrgID should be a pipe-separated list of all tenants
				// excluding any excluded ones
				for _, expected := range tt.expectedTenants {
					assert.Contains(t, resolvedOrgID, expected, "resolved org ID should contain %s", expected)
				}
			}
		})
	}
}

func containsWildcard(orgID string) bool {
	return orgID == "*" || len(orgID) > 1 && (orgID[0] == '*' || orgID[len(orgID)-1] == '*')
}

func TestWildcardTenantQuerier_resolveWildcardTenants_DiscoveryError(t *testing.T) {
	logger := log.NewNopLogger()

	discovery := &mockTenantDiscovery{
		err: ErrWildcardNotEnabled,
	}

	querier := &WildcardTenantQuerier{
		discovery: discovery,
		logger:    logger,
	}

	ctx := user.InjectOrgID(context.Background(), "*")
	_, err := querier.resolveWildcardTenants(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to discover tenants")
}

func TestWildcardTenantQuerier_resolveWildcardTenants_NoTenants(t *testing.T) {
	logger := log.NewNopLogger()

	// All tenants excluded
	discovery := &mockTenantDiscovery{
		tenants: []string{"tenant1"},
	}

	querier := &WildcardTenantQuerier{
		discovery: discovery,
		logger:    logger,
	}

	ctx := user.InjectOrgID(context.Background(), "*|!tenant1")
	_, err := querier.resolveWildcardTenants(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resolved to zero tenants")
}

func TestWildcardConfig(t *testing.T) {
	t.Run("config validation - wildcard without multi-tenant", func(t *testing.T) {
		cfg := Config{
			MultiTenantQueriesEnabled:    false,
			WildcardTenantQueriesEnabled: true,
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires querier.multi_tenant_queries_enabled")
	})

	t.Run("config validation - wildcard with multi-tenant", func(t *testing.T) {
		cfg := Config{
			MultiTenantQueriesEnabled:    true,
			WildcardTenantQueriesEnabled: true,
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})
}

// Test edge cases for tenant ID parsing
func TestParseWildcardOrgID(t *testing.T) {
	tests := []struct {
		name           string
		orgID          string
		hasWildcard    bool
		excludeTenants []string
	}{
		{
			name:           "simple wildcard",
			orgID:          "*",
			hasWildcard:    true,
			excludeTenants: nil,
		},
		{
			name:           "wildcard at start",
			orgID:          "*|!excluded",
			hasWildcard:    true,
			excludeTenants: []string{"excluded"},
		},
		{
			name:           "wildcard in middle (treated as separate tenant)",
			orgID:          "tenant1|*|tenant2",
			hasWildcard:    true,
			excludeTenants: nil,
		},
		{
			name:           "no wildcard",
			orgID:          "tenant1|tenant2",
			hasWildcard:    false,
			excludeTenants: nil,
		},
		{
			name:           "multiple exclusions",
			orgID:          "*|!t1|!t2|!t3",
			hasWildcard:    true,
			excludeTenants: []string{"t1", "t2", "t3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discovery := &StorageTenantDiscovery{}

			// Parse the org ID as the querier would
			ctx := user.InjectOrgID(context.Background(), tt.orgID)
			tenantIDs, err := extractTenantIDs(ctx)
			require.NoError(t, err)

			hasWildcard := false
			var excludeTenants []string

			for _, id := range tenantIDs {
				if discovery.IsWildcard(id) {
					hasWildcard = true
				} else if len(id) > 1 && id[0] == '!' {
					excludeTenants = append(excludeTenants, id[1:])
				}
			}

			assert.Equal(t, tt.hasWildcard, hasWildcard, "wildcard detection")
			assert.Equal(t, tt.excludeTenants, excludeTenants, "exclusion detection")
		})
	}
}

// Helper to extract tenant IDs (simulates what the querier does)
func extractTenantIDs(ctx context.Context) ([]string, error) {
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// Split by pipe character
	var tenants []string
	start := 0
	for i := 0; i <= len(orgID); i++ {
		if i == len(orgID) || orgID[i] == '|' {
			if i > start {
				tenants = append(tenants, orgID[start:i])
			}
			start = i + 1
		}
	}

	return tenants, nil
}
