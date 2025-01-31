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
		lineSizePerRetentionHours:  make(map[string]int),
		lineCountPerRetentionHours: make(map[string]int),
		tenantRetentionHours:       tenantRetentionHours,
	}
}

func (v *validationMetrics) compute(entry logproto.Entry, retentionHours string) {
	totalEntrySize := util.EntryTotalSize(&entry)
	v.lineSizePerRetentionHours[retentionHours] += totalEntrySize
	v.lineCountPerRetentionHours[retentionHours]++
	v.lineSize += totalEntrySize
	v.lineCount++
}
