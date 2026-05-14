package querier

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

// mockIndexClient implements storage.Client interface for testing
type mockIndexClient struct {
	tables  []string
	tenants map[string][]string // table -> tenants
	err     error
}

// Verify interface compliance
var _ storage.Client = (*mockIndexClient)(nil)

func (m *mockIndexClient) ListTables(ctx context.Context) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tables, nil
}

func (m *mockIndexClient) ListFiles(ctx context.Context, tableName string, _ bool) ([]storage.IndexFile, []string, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	tenants := m.tenants[tableName]
	return nil, tenants, nil
}

func (m *mockIndexClient) ListUserFiles(ctx context.Context, tableName, userID string, bypassCache bool) ([]storage.IndexFile, error) {
	return nil, nil
}

func (m *mockIndexClient) GetFile(ctx context.Context, tableName, fileName string) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockIndexClient) GetUserFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockIndexClient) PutFile(ctx context.Context, tableName, fileName string, file io.Reader) error {
	return nil
}

func (m *mockIndexClient) PutUserFile(ctx context.Context, tableName, userID, fileName string, file io.Reader) error {
	return nil
}

func (m *mockIndexClient) DeleteFile(ctx context.Context, tableName, fileName string) error {
	return nil
}

func (m *mockIndexClient) DeleteUserFile(ctx context.Context, tableName, userID, fileName string) error {
	return nil
}

func (m *mockIndexClient) RefreshIndexTableNamesCache(ctx context.Context) {}

func (m *mockIndexClient) RefreshIndexTableCache(ctx context.Context, tableName string) {}

func (m *mockIndexClient) IsFileNotFoundErr(err error) bool {
	return false
}

func (m *mockIndexClient) Stop() {}

func TestStorageTenantDiscovery_IsWildcard(t *testing.T) {
	discovery := &StorageTenantDiscovery{}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"wildcard", "*", true},
		{"single tenant", "tenant1", false},
		{"empty string", "", false},
		{"asterisk in name", "tenant*", false},
		{"multiple wildcards", "**", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := discovery.IsWildcard(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStorageTenantDiscovery_GetAllTenants(t *testing.T) {
	logger := log.NewNopLogger()

	tests := []struct {
		name            string
		tables          []string
		tenants         map[string][]string
		expectedTenants []string
		cacheTTL        time.Duration
	}{
		{
			name:   "single table single tenant",
			tables: []string{"table1"},
			tenants: map[string][]string{
				"table1": {"tenant1"},
			},
			expectedTenants: []string{"tenant1"},
			cacheTTL:        time.Minute,
		},
		{
			name:   "single table multiple tenants",
			tables: []string{"table1"},
			tenants: map[string][]string{
				"table1": {"tenant1", "tenant2", "tenant3"},
			},
			expectedTenants: []string{"tenant1", "tenant2", "tenant3"},
			cacheTTL:        time.Minute,
		},
		{
			name:   "multiple tables deduplicated tenants",
			tables: []string{"table1", "table2"},
			tenants: map[string][]string{
				"table1": {"tenant1", "tenant2"},
				"table2": {"tenant2", "tenant3"},
			},
			expectedTenants: []string{"tenant1", "tenant2", "tenant3"},
			cacheTTL:        time.Minute,
		},
		{
			name:   "empty tenants filtered",
			tables: []string{"table1"},
			tenants: map[string][]string{
				"table1": {"tenant1", "", "  ", "tenant2"},
			},
			expectedTenants: []string{"tenant1", "tenant2"},
			cacheTTL:        time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockIndexClient{
				tables:  tt.tables,
				tenants: tt.tenants,
			}

			discovery := NewStorageTenantDiscovery(client, logger, tt.cacheTTL)
			tenants, err := discovery.GetAllTenants(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tt.expectedTenants, tenants)
		})
	}
}

func TestStorageTenantDiscovery_Caching(t *testing.T) {
	logger := log.NewNopLogger()

	client := &mockIndexClient{
		tables: []string{"table1"},
		tenants: map[string][]string{
			"table1": {"tenant1"},
		},
	}

	discovery := NewStorageTenantDiscovery(client, logger, time.Minute)

	// First call should fetch from storage
	tenants1, err := discovery.GetAllTenants(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant1"}, tenants1)

	// Modify the underlying data - second call should return cached data
	client.tenants = map[string][]string{
		"table1": {"tenant1", "tenant2"},
	}

	tenants2, err := discovery.GetAllTenants(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant1"}, tenants2, "should return cached data")

	// Invalidate cache
	discovery.InvalidateCache()

	// Now should get fresh data
	tenants3, err := discovery.GetAllTenants(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant1", "tenant2"}, tenants3, "should return fresh data after invalidation")
}

func TestStorageTenantDiscovery_ConcurrentAccess(t *testing.T) {
	logger := log.NewNopLogger()
	client := &mockIndexClient{
		tables: []string{"table1"},
		tenants: map[string][]string{
			"table1": {"tenant1", "tenant2"},
		},
	}

	discovery := NewStorageTenantDiscovery(client, logger, time.Minute)

	var wg sync.WaitGroup
	concurrency := 100
	results := make([][]string, concurrency)
	errors := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tenants, err := discovery.GetAllTenants(context.Background())
			results[idx] = tenants
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// All results should be the same and no errors
	expected := []string{"tenant1", "tenant2"}
	for i := 0; i < concurrency; i++ {
		require.NoError(t, errors[i], "goroutine %d should not error", i)
		assert.Equal(t, expected, results[i], "goroutine %d should get correct tenants", i)
	}
}

func TestNoOpTenantDiscovery(t *testing.T) {
	discovery := &NoOpTenantDiscovery{}

	t.Run("IsWildcard always returns false", func(t *testing.T) {
		assert.False(t, discovery.IsWildcard("*"))
		assert.False(t, discovery.IsWildcard("tenant1"))
	})

	t.Run("GetAllTenants returns error", func(t *testing.T) {
		tenants, err := discovery.GetAllTenants(context.Background())
		assert.Nil(t, tenants)
		assert.ErrorIs(t, err, ErrWildcardNotEnabled)
	})
}

func TestDefaultCacheTTL(t *testing.T) {
	logger := log.NewNopLogger()
	client := &mockIndexClient{
		tables:  []string{"table1"},
		tenants: map[string][]string{"table1": {"tenant1"}},
	}

	// Test that 0 or negative TTL defaults to DefaultTenantCacheTTL
	discovery := NewStorageTenantDiscovery(client, logger, 0)
	assert.Equal(t, DefaultTenantCacheTTL, discovery.cacheTTL)

	discovery2 := NewStorageTenantDiscovery(client, logger, -1*time.Minute)
	assert.Equal(t, DefaultTenantCacheTTL, discovery2.cacheTTL)
}
