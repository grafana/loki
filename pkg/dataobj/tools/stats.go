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
	Size                   uint64
	Sections               int
	SectionSizes           []uint64
	Tenants                []string
	TenantSections         map[string]int
	SectionsPerTenantStats PercentileStats
	SectionSizeStats       PercentileStats
}

type PercentileStats struct {
	Median float64
	P95    float64
	P99    float64
}

// ReadStats returns statistics about the data object. ReadStats returns an
// error if the data object couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(_ context.Context, obj *dataobj.Object) (*Stats, error) {
	s := Stats{}
	s.Size = uint64(obj.Size())
	s.TenantSections = make(map[string]int)
	for _, sec := range obj.Sections() {
		s.Sections++
		s.SectionSizes = append(s.SectionSizes, uint64(sec.Reader.DataSize()+sec.Reader.MetadataSize()))
		s.Tenants = append(s.Tenants, sec.Tenant)
		s.TenantSections[sec.Tenant]++
	}
	// A tenant can have multiple sections, so we must deduplicate them.
	slices.Sort(s.Tenants)
	s.Tenants = slices.Compact(s.Tenants)
	calcSectionsPerTenantStats(&s)
	calcSectionSizeStats(&s)
	return &s, nil
}

// calcSectionsPerTenantStats calculates the median and other
// percentiles for the number of sections per tenant.
func calcSectionsPerTenantStats(s *Stats) {
	if len(s.TenantSections) == 0 {
		return
	}
	counts := make([]int, 0, len(s.TenantSections))
	for _, n := range s.TenantSections {
		counts = append(counts, n)
	}
	calcPercentiles(counts, &s.SectionsPerTenantStats)
}

// calcSectionSizeStats calculates the median and other percentiles for the
// size of sections.
func calcSectionSizeStats(s *Stats) {
	if len(s.SectionSizes) == 0 {
		return
	}
	// Make a copy of the section sizes.
	sizes := make([]uint64, len(s.SectionSizes))
	copy(sizes, s.SectionSizes)
	calcPercentiles(sizes, &s.SectionSizeStats)
}

func calcPercentiles[T int | uint | int64 | uint64](input []T, s *PercentileStats) {
	// Data must be sorted to calculate percentiles.
	slices.Sort(input)
	n := len(input)
	if n%2 == 0 {
		s.Median = float64(input[n/2-1]+input[n/2]) / 2.0
	} else {
		s.Median = float64(input[n/2])
	}
	idx := int(float64(n) * 0.95)
	if idx >= n {
		idx = n - 1
	}
	s.P95 = float64(input[idx])
	idx = int(float64(n) * 0.99)
	if idx >= n {
		idx = n - 1
	}
	s.P99 = float64(input[idx])
}
