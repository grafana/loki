package distributor

import (
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type pushStats struct {
	lineSize  int
	lineCount int
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

func (v *validationMetrics) compute(entry logproto.Entry, retentionHours string, policy string) {
	if _, ok := v.policyPushStats[policy]; !ok {
		v.policyPushStats[policy] = make(map[string]pushStats)
	}

	if _, ok := v.policyPushStats[policy][retentionHours]; !ok {
		v.policyPushStats[policy][retentionHours] = pushStats{}
	}

	totalEntrySize := util.EntryTotalSize(&entry)

	v.aggregatedPushStats.lineSize += totalEntrySize
	v.aggregatedPushStats.lineCount++

	stats := v.policyPushStats[policy][retentionHours]
	stats.lineCount++
	stats.lineSize += totalEntrySize
	v.policyPushStats[policy][retentionHours] = stats
}
