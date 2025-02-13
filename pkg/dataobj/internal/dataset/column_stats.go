package dataset

import (
	"fmt"

	"github.com/axiomhq/hyperloglog"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// NB: https://engineering.fb.com/2018/12/13/data-infrastructure/hyperloglog/
// Standard error (SE) = 1.04/sqrt(2^n_registers)
// so
//
// 65% of estimates will be within  ±1SE of true value
// 95% of estimates will be within  ±2SE of true value
// 99% of estimates will be within  ±3SE of true value
//
// e.g. given 2^12 registers
// SE = 1.04/sqrt(2^12) = 0.01625
// 65% of estimates will be within  ±1SE of true value (1.625%)
// 95% of estimates will be within  ±2SE of true value (3.25%)
// 99% of estimates will be within  ±3SE of true value (4.875%)
// and with 8-bit registers, this is 2^12 = 4KB size.
func newHyperLogLog() (*hyperloglog.Sketch, error) {
	return hyperloglog.NewSketch(12, true)
}

type columnStatsBuilder struct {
	opts StatisticsOptions

	// for cardinality
	hll *hyperloglog.Sketch
}

// ColumnStatsBuilder is for column-level statistics
func newColumnStatsBuilder(opts StatisticsOptions) (*columnStatsBuilder, error) {
	result := &columnStatsBuilder{
		opts: opts,
	}

	if opts.StoreCardinalityStats {
		var err error
		if result.hll, err = newHyperLogLog(); err != nil {
			return nil, fmt.Errorf("failed to create hll: %w", err)
		}
	}

	return result, nil
}

func (csb *columnStatsBuilder) Append(value Value) {
	if csb.opts.StoreCardinalityStats && !value.IsNil() && !value.IsZero() {
		buf, err := value.MarshalBinary()
		if err != nil {
			panic(fmt.Sprintf(
				"failed to marshal value for cardinality stats of type %s: %s",
				value.Type(), err,
			))
		}

		// TODO(owen-d): improve efficiency, ideally we don't need to marshal
		// into an intermediate buffer.
		csb.hll.Insert(buf)
	}
}

// Flush builds the column-level stats both from the given pages and any internal
// state
func (csb *columnStatsBuilder) Flush(pages []*MemPage) *datasetmd.Statistics {
	var dst datasetmd.Statistics
	if csb.opts.StoreCardinalityStats {
		dst.CardinalityCount = csb.hll.Estimate()
	}
	if csb.opts.StoreRangeStats {
		csb.buildRangeStats(pages, &dst)
	}

	return &dst
}

func (csb *columnStatsBuilder) buildRangeStats(pages []*MemPage, dst *datasetmd.Statistics) {
	var minValue, maxValue Value

	for i, page := range pages {
		if page.Info.Stats == nil {
			// This should never hit; if cb.opts.StoreRangeStats is true, then
			// page.Info.Stats will be populated.
			panic("ColumnStatsBuilder.buildStats: page missing stats")
		}

		var pageMin, pageMax Value

		if err := pageMin.UnmarshalBinary(page.Info.Stats.MinValue); err != nil {
			panic(fmt.Sprintf("ColumnStatsBuilder.buildStats: failed to unmarshal min value: %s", err))
		} else if err := pageMax.UnmarshalBinary(page.Info.Stats.MaxValue); err != nil {
			panic(fmt.Sprintf("ColumnStatsBuilder.buildStats: failed to unmarshal max value: %s", err))
		}

		if i == 0 || CompareValues(pageMin, minValue) < 0 {
			minValue = pageMin
		}
		if i == 0 || CompareValues(pageMax, maxValue) > 0 {
			maxValue = pageMax
		}
	}

	var err error
	if dst.MinValue, err = minValue.MarshalBinary(); err != nil {
		panic(fmt.Sprintf("ColumnStatsBuilder.buildStats: failed to marshal min value: %s", err))
	}
	if dst.MaxValue, err = maxValue.MarshalBinary(); err != nil {
		panic(fmt.Sprintf("ColumnStatsBuilder.buildStats: failed to marshal max value: %s", err))
	}
}
