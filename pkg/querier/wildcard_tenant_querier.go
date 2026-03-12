package querier

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier/pattern"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

var (
	// ErrExcludeWithoutWildcard is returned when tenant exclusion syntax is used
	// without a wildcard
	ErrExcludeWithoutWildcard = errors.New("tenant exclusion (!) syntax requires wildcard (*) to be present")
)

// WildcardTenantQuerier wraps a MultiTenantQuerier and provides wildcard tenant support.
// It resolves "*" to all available tenants from storage before delegating to the
// underlying querier.
//
// Supported X-Scope-OrgID formats:
//   - "*"           : Query all tenants
//   - "*|!tenant1"  : Query all tenants except tenant1
//   - "*|!t1|!t2"   : Query all tenants except t1 and t2
type WildcardTenantQuerier struct {
	*MultiTenantQuerier
	discovery TenantDiscovery
	logger    log.Logger
}

// WildcardConfig holds configuration for wildcard tenant queries
type WildcardConfig struct {
	// Enabled enables wildcard tenant queries
	Enabled bool `yaml:"enabled"`

	// CacheTTL is how long to cache the list of discovered tenants
	CacheTTL string `yaml:"cache_ttl"`
}

// NewWildcardTenantQuerier creates a new WildcardTenantQuerier.
func NewWildcardTenantQuerier(querier *MultiTenantQuerier, discovery TenantDiscovery, logger log.Logger) *WildcardTenantQuerier {
	return &WildcardTenantQuerier{
		MultiTenantQuerier: querier,
		discovery:          discovery,
		logger:             logger,
	}
}

// resolveWildcardTenants checks if the context contains a wildcard tenant and
// resolves it to actual tenant IDs. Returns a new context with resolved tenants
// injected, or the original context if no wildcard is present.
func (q *WildcardTenantQuerier) resolveWildcardTenants(ctx context.Context) (context.Context, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return ctx, err
	}

	// Check if we have a wildcard
	hasWildcard := false
	excludeTenants := make(map[string]struct{})

	for _, id := range tenantIDs {
		if q.discovery.IsWildcard(id) {
			hasWildcard = true
		} else if strings.HasPrefix(id, "!") {
			// Tenant exclusion syntax: !tenant_id
			excludeTenants[strings.TrimPrefix(id, "!")] = struct{}{}
		}
	}

	// If exclusions are specified without wildcard, return error
	if len(excludeTenants) > 0 && !hasWildcard {
		return ctx, ErrExcludeWithoutWildcard
	}

	// If no wildcard, return original context
	if !hasWildcard {
		return ctx, nil
	}

	// Resolve wildcard to all tenants
	allTenants, err := q.discovery.GetAllTenants(ctx)
	if err != nil {
		return ctx, fmt.Errorf("failed to discover tenants for wildcard query: %w", err)
	}

	// Apply exclusions
	var resolvedTenants []string
	for _, t := range allTenants {
		if _, excluded := excludeTenants[t]; !excluded {
			resolvedTenants = append(resolvedTenants, t)
		}
	}

	if len(resolvedTenants) == 0 {
		return ctx, errors.New("wildcard query resolved to zero tenants after exclusions")
	}

	level.Info(q.logger).Log(
		"msg", "resolved wildcard tenant query",
		"total_tenants", len(allTenants),
		"excluded_tenants", len(excludeTenants),
		"resolved_tenants", len(resolvedTenants),
	)

	// Inject resolved tenants into context
	// The tenant package expects the org ID header format (pipe-separated)
	orgID := strings.Join(resolvedTenants, "|")
	return user.InjectOrgID(ctx, orgID), nil
}

// SelectLogs implements Querier.SelectLogs with wildcard support
func (q *WildcardTenantQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.SelectLogs(ctx, params)
}

// SelectSamples implements Querier.SelectSamples with wildcard support
func (q *WildcardTenantQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.SelectSamples(ctx, params)
}

// Label implements Querier.Label with wildcard support
func (q *WildcardTenantQuerier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.Label(ctx, req)
}

// Series implements Querier.Series with wildcard support
func (q *WildcardTenantQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.Series(ctx, req)
}

// IndexStats implements Querier.IndexStats with wildcard support
func (q *WildcardTenantQuerier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.IndexStats(ctx, req)
}

// IndexShards implements Querier.IndexShards with wildcard support
func (q *WildcardTenantQuerier) IndexShards(ctx context.Context, req *loghttp.RangeQuery, targetBytesPerShard uint64) (*logproto.ShardsResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.IndexShards(ctx, req, targetBytesPerShard)
}

// Volume implements Querier.Volume with wildcard support
func (q *WildcardTenantQuerier) Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.Volume(ctx, req)
}

// Patterns implements Querier.Patterns with wildcard support
func (q *WildcardTenantQuerier) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.Patterns(ctx, req)
}

// DetectedFields implements Querier.DetectedFields with wildcard support
func (q *WildcardTenantQuerier) DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.DetectedFields(ctx, req)
}

// DetectedLabels implements Querier.DetectedLabels with wildcard support
func (q *WildcardTenantQuerier) DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error) {
	ctx, err := q.resolveWildcardTenants(ctx)
	if err != nil {
		return nil, err
	}
	return q.MultiTenantQuerier.DetectedLabels(ctx, req)
}

// WithPatternQuerier implements Querier.WithPatternQuerier
func (q *WildcardTenantQuerier) WithPatternQuerier(patternQuerier pattern.PatterQuerier) {
	q.MultiTenantQuerier.WithPatternQuerier(patternQuerier)
}
