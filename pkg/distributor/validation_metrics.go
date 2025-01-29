package distributor

import (
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type validationMetrics struct {
	lineSizePerRetentionHours  map[string]int
	lineCountPerRetentionHours map[string]int
	lineSize                   int
	lineCount                  int
	tenantRetentionHours       string
}

func newValidationMetrics(tenantRetentionHours string) validationMetrics {
	return validationMetrics{
		policyPushStats:      make(map[string]map[string]pushRetentionStats),
		tenantRetentionHours: tenantRetentionHours,
	}
}

func (v *validationMetrics) compute(entry logproto.Entry, retentionHours string, policy string) {
	if _, ok := v.policyPushStats[policy]; !ok {
		v.policyPushStats[policy] = make(map[string]pushRetentionStats)
	}

	if _, ok := v.policyPushStats[policy][retentionHours]; !ok {
		v.policyPushStats[policy][retentionHours] = pushRetentionStats{}
	}

	totalEntrySize := util.EntryTotalSize(&entry)

	v.lineSize += totalEntrySize
	v.lineCount++

	stats := v.policyPushStats[policy][retentionHours]
	stats.lineCount++
	stats.lineSize += totalEntrySize
	v.policyPushStats[policy][retentionHours] = stats
}
