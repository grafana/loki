package main

import (
	"context"
	"fmt"

	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

func analyze(shipper indexshipper.IndexShipper, tableName string, tenants []string) error {

	var (
		series    int
		chunks    int
		seriesRes []tsdb.Series
		chunkRes  []tsdb.ChunkRef
	)
	for _, tenant := range tenants {
		fmt.Println(fmt.Sprintf("analyzing tenant %s", tenant))
		err := shipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			index.ForEachIndexCallback(func(isMultiTenantIndex bool, idx index.Index) error {
				if isMultiTenantIndex {
					return nil
				}

				casted := idx.(*tsdb.TSDBFile)
				seriesRes = seriesRes[:0]
				chunkRes = chunkRes[:0]

				res, err := casted.Series(
					context.Background(),
					tenant,
					model.Earliest,
					model.Latest,
					seriesRes, nil,
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				if err != nil {
					return err
				}

				series += len(res)

				chunkRes, err := casted.GetChunkRefs(
					context.Background(),
					tenant,
					model.Earliest,
					model.Latest,
					chunkRes, nil,
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				if err != nil {
					return err
				}

				chunks += len(chunkRes)

				return nil
			}),
		)

		if err != nil {
			return err
		}
	}

	fmt.Println(fmt.Sprintf("analyzed %d series and %d chunks for an average of %f chunks per series", series, chunks, float64(chunks)/float64(series)))

	return nil
}

// series [avg, p50, p90, p99, max]
// chunks per series[avg, p50, p90, p99, max]
