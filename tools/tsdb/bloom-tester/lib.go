package main

import (
	"context"
	"flag"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	indexshipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/tools/tsdb/helpers"
)

func execute() {
	conf, bucket, err := helpers.Setup()
	helpers.ExitErr("setting up", err)

	_, overrides, clientMetrics := helpers.DefaultConfigs()

	flag.Parse()

	objectClient, err := storage.NewObjectClient(conf.StorageConfig.TSDBShipperConfig.SharedStoreType, conf.StorageConfig, clientMetrics)
	helpers.ExitErr("creating object client", err)

	chunkClient := client.NewClient(objectClient, nil, conf.SchemaConfig)

	tableRanges := helpers.GetIndexStoreTableRanges(config.TSDBType, conf.SchemaConfig.Configs)

	openFn := func(p string) (indexshipper_index.Index, error) {
		return tsdb.OpenShippableTSDB(p, tsdb.IndexOpts{})
	}

	shipper, err := indexshipper.NewIndexShipper(
		conf.StorageConfig.TSDBShipperConfig.Config,
		objectClient,
		overrides,
		nil,
		openFn,
		tableRanges[len(tableRanges)-1],
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", prometheus.DefaultRegisterer),
		util_log.Logger,
	)
	helpers.ExitErr("creating index shipper", err)

	tenants, tableName, err := helpers.ResolveTenants(objectClient, bucket, tableRanges)
	helpers.ExitErr("resolving tenants", err)

	sampler, err := NewProbabilisticSampler(0.001)
	helpers.ExitErr("creating sampler", err)

	err = analyze(sampler, shipper, chunkClient, tableName, tenants)
	helpers.ExitErr("analyzing", err)
}

func analyze(sampler Sampler, shipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string) error {
	for _, tenant := range tenants {
		shipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			indexshipper_index.ForEachIndexCallback(func(isMultiTenantIndex bool, idx indexshipper_index.Index) error {
				if isMultiTenantIndex {
					return nil
				}

				casted := idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex)
				casted.ForSeries(
					context.Background(),
					nil, model.Earliest, model.Latest,
					func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
						if !sampler.Sample() {
							return
						}

						transformed := make([]chunk.Chunk, 0, len(chks))
						for _, chk := range chks {
							transformed = append(transformed, chunk.Chunk{
								ChunkRef: logproto.ChunkRef{
									Fingerprint: uint64(fp),
									UserID:      tenant,
									From:        chk.From(),
									Through:     chk.Through(),
									Checksum:    chk.Checksum,
								},
							})
						}

						got, err := client.GetChunks(
							context.Background(),
							transformed,
						)
						helpers.ExitErr("getting chunks", err)

						for _, c := range got {
							itr, err := c.Data.(*chunkenc.Facade).LokiChunk().Iterator(
								context.Background(),
								model.Earliest.Time(), model.Latest.Time(), logproto.FORWARD,
								log.NewNoopPipeline().ForStream(ls),
							)
							helpers.ExitErr("getting iterator", err)

							for itr.Next() {
								itr.Entry()
							}
						}

					},
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				return nil

			}),
		)
	}

}
