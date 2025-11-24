package index

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

type streamStatisticsCalculation struct{}

func (c *streamStatisticsCalculation) Prepare(ctx context.Context, section *dataobj.Section, stats logs.Stats) error {
	return nil
}

func (c *streamStatisticsCalculation) ProcessBatch(ctx context.Context, context *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		err := context.builder.ObserveLogLine(context.tenantID, context.objectPath, context.sectionIdx, log.StreamID, context.streamIDLookup[log.StreamID], log.Timestamp, int64(len(log.Line)))
		if err != nil {
			return fmt.Errorf("failed to observe log line: %w", err)
		}
	}
	return nil
}

func (c *streamStatisticsCalculation) Flush(ctx context.Context, context *logsCalculationContext) error {
	return nil
}
