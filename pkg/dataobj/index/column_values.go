package index

import (
	"context"
	"fmt"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

type columnValuesCalculation struct {
	columnBloomBuilders map[string]*bloom.BloomFilter
	columnIndexes       map[string]int64
}

func (c *columnValuesCalculation) Prepare(_ context.Context, _ *dataobj.Section, stats logs.Stats) error {
	c.columnBloomBuilders = make(map[string]*bloom.BloomFilter)
	c.columnIndexes = make(map[string]int64)

	for _, column := range stats.Columns {
		logsType, _ := logs.ParseColumnType(column.Type)
		if logsType != logs.ColumnTypeMetadata {
			continue
		}
		c.columnBloomBuilders[column.Name] = bloom.NewWithEstimates(uint(column.Cardinality), 1.0/128.0)
		c.columnIndexes[column.Name] = column.ColumnIndex
	}
	return nil
}

func (c *columnValuesCalculation) ProcessBatch(_ context.Context, _ *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		log.Metadata.Range(func(md labels.Label) {
			c.columnBloomBuilders[md.Name].Add([]byte(md.Value))
		})
	}
	return nil
}

func (c *columnValuesCalculation) Flush(_ context.Context, context *logsCalculationContext) error {
	for columnName, bloom := range c.columnBloomBuilders {
		bloomBytes, err := bloom.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to marshal bloom filter: %w", err)
		}
		err = context.builder.AppendColumnIndex(context.tenantID, context.objectPath, context.sectionIdx, columnName, c.columnIndexes[columnName], bloomBytes)
		if err != nil {
			return fmt.Errorf("failed to append column index: %w", err)
		}
	}
	return nil
}
