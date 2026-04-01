package index

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

type streamStatisticsCalculation struct{}

func (c *streamStatisticsCalculation) Prepare(_ context.Context, _ *dataobj.Section, _ logs.Stats) error {
	return nil
}

func (c *streamStatisticsCalculation) ProcessBatch(_ context.Context, context *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		err := context.builder.ObserveLogLine(context.tenantID, context.objectPath, context.sectionIdx, log.StreamID, context.streamIDLookup[log.StreamID], time.Unix(0, log.TimestampNano), int64(len(log.Line)))
		if err != nil {
			return fmt.Errorf("failed to observe log line: %w", err)
		}
	}
	return nil
}

func (c *streamStatisticsCalculation) Flush(_ context.Context, _ *logsCalculationContext) error {
	return nil
}
