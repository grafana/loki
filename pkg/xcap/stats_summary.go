package xcap

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// Region name for data object scan operations.
const regionNameDataObjScan = "DataObjScan"

// Statistic keys used for Summary computation.
// These must match the statistic names used in the executor package.
const (
	// Row statistics
	statKeyRowsOut         = "rows_out"
	statKeyPrimaryRowsRead = "primary_rows_read"

	// Byte statistics
	statKeyPrimaryColumnUncompressedBytes   = "primary_column_uncompressed_bytes"
	statKeySecondaryColumnUncompressedBytes = "secondary_column_uncompressed_bytes"
)

// ToStatsSummary computes a stats.Summary from observations in the capture.
func (c *Capture) ToStatsSummary(execTime, queueTime time.Duration, totalEntriesReturned int) stats.Summary {
	summary := stats.Summary{
		ExecTime:             execTime.Seconds(),
		QueueTime:            queueTime.Seconds(),
		TotalEntriesReturned: int64(totalEntriesReturned),
	}

	if c == nil {
		return summary
	}

	// Collect observations from DataObjScan as the summary stats mainly relate to log lines.
	// In practice, new engine would process more bytes while scanning metastore objects and stream sections.
	collector := newObservationCollector(c)
	obs := collector.fromRegions(regionNameDataObjScan, true).filter(
		statKeyRowsOut,
		statKeyPrimaryRowsRead,
		statKeyPrimaryColumnUncompressedBytes,
		statKeySecondaryColumnUncompressedBytes,
	)

	// TotalBytesProcessed: sum of uncompressed bytes from primary and secondary columns
	summary.TotalBytesProcessed = getObservation(obs, statKeyPrimaryColumnUncompressedBytes) +
		getObservation(obs, statKeySecondaryColumnUncompressedBytes)

	// TotalLinesProcessed: primary rows read
	summary.TotalLinesProcessed = getObservation(obs, statKeyPrimaryRowsRead)

	// TotalPostFilterLines: rows output after filtering
	// TODO: this will report the wrong value if the plan has a filter stage.
	// pick the min of row_out from filter and scan nodes.
	summary.TotalPostFilterLines = getObservation(obs, statKeyRowsOut)

	if execTime > 0 {
		execSeconds := execTime.Seconds()
		summary.BytesProcessedPerSecond = int64(float64(summary.TotalBytesProcessed) / execSeconds)
		summary.LinesProcessedPerSecond = int64(float64(summary.TotalLinesProcessed) / execSeconds)
	}

	return summary
}

// getObservation retrieves an int64 value from observations by statistic name.
func getObservation(obs *observations, name string) int64 {
	if obs == nil {
		return 0
	}

	for key, agg := range obs.data {
		if key.Name == name {
			if v, ok := agg.Value.(int64); ok {
				return v
			}
		}
	}
	return 0
}
