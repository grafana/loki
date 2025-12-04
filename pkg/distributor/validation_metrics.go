package distributor

import (
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type pushStats struct {
	lineSize  int
	lineCount int

	lineSizeWithoutResourceAndScopeAttributes int
}

type validationMetrics struct {
	policyPushStats      map[string]map[string]pushStats // policy -> retentionHours -> lineSize
	tenantRetentionHours string
	aggregatedPushStats  pushStats
}

func newValidationMetrics(tenantRetentionHours string) validationMetrics {
	return validationMetrics{
		policyPushStats:      make(map[string]map[string]pushStats),
		tenantRetentionHours: tenantRetentionHours,
	}
}

func (v *validationMetrics) compute(entry logproto.Entry, retentionHours string, policy string, resourceAndScopeAttributes push.LabelsAdapter) {
	if _, ok := v.policyPushStats[policy]; !ok {
		v.policyPushStats[policy] = make(map[string]pushStats)
	}

	if _, ok := v.policyPushStats[policy][retentionHours]; !ok {
		v.policyPushStats[policy][retentionHours] = pushStats{}
	}

	totalEntrySize := util.EntryTotalSize(&entry, nil)
	totalEntrySizeWithoutResourceAndScopeAttributes := totalEntrySize
	if len(resourceAndScopeAttributes) != 0 {
		totalEntrySizeWithoutResourceAndScopeAttributes = util.EntryTotalSize(&entry, resourceAndScopeAttributes)
	}

	v.aggregatedPushStats.lineSize += totalEntrySize
	v.aggregatedPushStats.lineSizeWithoutResourceAndScopeAttributes += totalEntrySizeWithoutResourceAndScopeAttributes
	v.aggregatedPushStats.lineCount++

	stats := v.policyPushStats[policy][retentionHours]
	stats.lineCount++
	stats.lineSize += totalEntrySize
	stats.lineSizeWithoutResourceAndScopeAttributes += totalEntrySizeWithoutResourceAndScopeAttributes
	v.policyPushStats[policy][retentionHours] = stats
}
