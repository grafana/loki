// TODO(grobinson): Find a way to move this file into the dataobj package.
// Today it's not possible because there is a circular dependency between the
// dataobj package and the logs package.
package tools

import (
	"context"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

type Stats struct {
	Size           uint64
	Sections       int
	Tenants        []string
	TenantSections map[string]int
	SectionsPerTenantStats
}

type SectionsPerTenantStats struct {
	Median float64
	P95    float64
	P99    float64
}

// ReadStats returns statistics about the data object. ReadStats returns an
// error if the data object couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(ctx context.Context, obj *dataobj.Object) (*Stats, error) {
	s := Stats{}
	s.Size = uint64(obj.Size())
	s.TenantSections = make(map[string]int)
	for _, sec := range obj.Sections() {
		s.Sections++
		s.Tenants = append(s.Tenants, sec.Tenant)
	}
	// A tenant can have multiple sections, so we must deduplicate them.
	slices.Sort(s.Tenants)
	s.Tenants = slices.Compact(s.Tenants)
	calculateSectionsPerTenantStats(&s)
	return &s, nil
}

func calculateSectionsPerTenantStats(s *Stats) {
	if len(s.TenantSections) == 0 {
		return
	}
	counts := make([]int, 0, len(s.TenantSections))
	for _, n := range s.TenantSections {
		counts = append(counts, n)
	}
	// Data must be sorted to calculate percentiles.
	slices.Sort(counts)
	n := len(counts)
	if n%2 == 0 {
		s.SectionsPerTenantStats.Median = float64(counts[n/2-1]+counts[n/2]) / 2.0
	} else {
		s.SectionsPerTenantStats.Median = float64(counts[n/2])
	}
	idx := int(float64(n) * 0.95)
	if idx >= n {
		idx = n - 1
	}
	s.SectionsPerTenantStats.P95 = float64(counts[idx])
	idx = int(float64(n) * 0.99)
	if idx >= n {
		idx = n - 1
	}
	s.SectionsPerTenantStats.P99 = float64(counts[idx])
}
