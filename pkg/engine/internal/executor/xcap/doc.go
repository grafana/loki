// Package xcap provides a utility to capture statistical information about the
// lifetime of a query.
//
// Execution captures allow Loki developers to explore statistics at different
// granularities, far beyond what is possible compared to the existing
// logqlmodel/stats. They hold information for:
//
//   - Timing: how much wall clock time was spent performing a specific operation?
//   - Statistics: how many bytes were processed by a node? How many lines did it filter out?
//   - Execution info: what machine executed an operation? Did anything fail?
//
// Basic usage:
//
//	ctx, capture := xcap.NewCapture(context.Background(), nil)
//	defer capture.End()
//
//	ctx, region := xcap.StartRegion(ctx, "work")
//	defer region.End()
//
//	region.Record(bytesRead.Observe(1024))
package xcap

