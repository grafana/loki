package querier

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

var (
	// ErrWildcardNotEnabled is returned when wildcard tenant queries are attempted
	// but the feature is not enabled
	ErrWildcardNotEnabled = errors.New("wildcard tenant queries are not enabled")
)

const (
	// WildcardTenantID is the special tenant ID that represents all tenants
	WildcardTenantID = "*"

	// DefaultTenantCacheTTL is the default time-to-live for the tenant cache
	DefaultTenantCacheTTL = 5 * time.Minute
)

// TenantDiscovery provides methods to discover all available tenants
// from storage. This is used to support wildcard tenant queries.
type TenantDiscovery interface {
	// GetAllTenants returns all known tenant IDs from storage.
	// This operation may be expensive and should be cached.
	GetAllTenants(ctx context.Context) ([]string, error)

	// IsWildcard returns true if the given orgID represents a wildcard query
	IsWildcard(orgID string) bool
}

// StorageTenantDiscovery discovers tenants by listing them from the index storage.
// It caches the results to avoid expensive storage operations on every query.
type StorageTenantDiscovery struct {
	indexClient storage.Client
	logger      log.Logger
	cacheTTL    time.Duration

	mu            sync.RWMutex
	cachedTenants []string
	cacheTime     time.Time
}

// TenantDiscoveryConfig holds configuration for tenant discovery
type TenantDiscoveryConfig struct {
	// CacheTTL is how long to cache the list of tenants
	CacheTTL time.Duration `yaml:"cache_ttl"`

	// Enabled controls whether wildcard tenant queries are allowed
	Enabled bool `yaml:"enabled"`
}

// NewStorageTenantDiscovery creates a new StorageTenantDiscovery instance
func NewStorageTenantDiscovery(indexClient storage.Client, logger log.Logger, cacheTTL time.Duration) *StorageTenantDiscovery {
	if cacheTTL <= 0 {
		cacheTTL = DefaultTenantCacheTTL
	}

	return &StorageTenantDiscovery{
		indexClient: indexClient,
		logger:      logger,
		cacheTTL:    cacheTTL,
	}
}

// IsWildcard returns true if the orgID is the wildcard character
func (s *StorageTenantDiscovery) IsWildcard(orgID string) bool {
	return orgID == WildcardTenantID
}

// GetAllTenants returns all known tenant IDs from storage.
// Results are cached for cacheTTL duration to avoid expensive storage operations.
func (s *StorageTenantDiscovery) GetAllTenants(ctx context.Context) ([]string, error) {
	// Check cache first
	s.mu.RLock()
	if s.cachedTenants != nil && time.Since(s.cacheTime) < s.cacheTTL {
		tenants := make([]string, len(s.cachedTenants))
		copy(tenants, s.cachedTenants)
		s.mu.RUnlock()
		return tenants, nil
	}
	s.mu.RUnlock()

	// Cache miss or expired, fetch from storage
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if s.cachedTenants != nil && time.Since(s.cacheTime) < s.cacheTTL {
		tenants := make([]string, len(s.cachedTenants))
		copy(tenants, s.cachedTenants)
		return tenants, nil
	}

	tenants, err := s.discoverTenants(ctx)
	if err != nil {
		return nil, err
	}

	s.cachedTenants = tenants
	s.cacheTime = time.Now()

	// Return a copy to prevent modification
	result := make([]string, len(tenants))
	copy(result, tenants)

	level.Info(s.logger).Log(
		"msg", "discovered tenants for wildcard query",
		"count", len(tenants),
	)

	return result, nil
}

// discoverTenants fetches all tenant IDs from the index storage
func (s *StorageTenantDiscovery) discoverTenants(ctx context.Context) ([]string, error) {
	tables, err := s.indexClient.ListTables(ctx)
	if err != nil {
		return nil, err
	}

	tenantSet := make(map[string]struct{})

	for _, table := range tables {
		_, tenants, err := s.indexClient.ListFiles(ctx, table, true)
		if err != nil {
			level.Warn(s.logger).Log(
				"msg", "failed to list tenants for table",
				"table", table,
				"err", err,
			)
			continue
		}

		for _, tenant := range tenants {
			// Skip empty tenant IDs
			if strings.TrimSpace(tenant) == "" {
				continue
			}
			tenantSet[tenant] = struct{}{}
		}
	}

	// Convert set to sorted slice
	tenants := make([]string, 0, len(tenantSet))
	for tenant := range tenantSet {
		tenants = append(tenants, tenant)
	}
	sort.Strings(tenants)

	return tenants, nil
}

// InvalidateCache forces the cache to be refreshed on the next GetAllTenants call
func (s *StorageTenantDiscovery) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedTenants = nil
	s.cacheTime = time.Time{}
}

// NoOpTenantDiscovery is a no-op implementation that doesn't support wildcards
type NoOpTenantDiscovery struct{}

// IsWildcard always returns false for NoOpTenantDiscovery
func (n *NoOpTenantDiscovery) IsWildcard(_ string) bool {
	return false
}

// GetAllTenants returns an error indicating wildcards are not supported
func (n *NoOpTenantDiscovery) GetAllTenants(_ context.Context) ([]string, error) {
	return nil, ErrWildcardNotEnabled
}
