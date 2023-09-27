package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/go-kit/log/level"
	"github.com/owen-d/BoomFilters/boom"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/services"
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
	conf, svc, bucket, err := helpers.Setup()
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
		conf.StorageConfig.MaxParallelTableOps,
	)
	helpers.ExitErr("creating index shipper", err)

	tenants, tableName, err := helpers.ResolveTenants(objectClient, bucket, tableRanges)
	helpers.ExitErr("resolving tenants", err)

	sampler, err := NewProbabilisticSampler(0.00008)
	helpers.ExitErr("creating sampler", err)

	metrics := NewMetrics(prometheus.DefaultRegisterer)

	level.Info(util_log.Logger).Log("msg", "starting server")
	err = services.StartAndAwaitRunning(context.Background(), svc)
	helpers.ExitErr("waiting for service to start", err)
	level.Info(util_log.Logger).Log("msg", "server started")

	err = analyze(metrics, sampler, shipper, chunkClient, tableName, tenants)
	helpers.ExitErr("analyzing", err)
}

var (
	three      = newNGramTokenizer(3, 4, 0)
	threeSkip1 = newNGramTokenizer(3, 4, 1)
	threeSkip2 = newNGramTokenizer(3, 4, 2)
	threeSkip3 = newNGramTokenizer(3, 4, 3)

	onePctError  = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.01, 0.8) }
	fivePctError = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.05, 0.8) }
)

var experiments = []Experiment{
	// n > error > skip > index

	NewExperiment(
		"token=3skip0_error=1%_indexchunks=true",
		three,
		true,
		onePctError,
	),
	NewExperiment(
		"token=3skip0_error=1%_indexchunks=false",
		three,
		false,
		onePctError,
	),

	NewExperiment(
		"token=3skip1_error=1%_indexchunks=true",
		threeSkip1,
		true,
		onePctError,
	),
	NewExperiment(
		"token=3skip1_error=1%_indexchunks=false",
		threeSkip1,
		false,
		onePctError,
	),

	NewExperiment(
		"token=3skip2_error=1%_indexchunks=true",
		threeSkip2,
		true,
		onePctError,
	),
	NewExperiment(
		"token=3skip2_error=1%_indexchunks=false",
		threeSkip2,
		false,
		onePctError,
	),

	NewExperiment(
		"token=3skip0_error=5%_indexchunks=true",
		three,
		true,
		fivePctError,
	),
	NewExperiment(
		"token=3skip0_error=5%_indexchunks=false",
		three,
		false,
		fivePctError,
	),

	NewExperiment(
		"token=3skip1_error=5%_indexchunks=true",
		threeSkip1,
		true,
		fivePctError,
	),
	NewExperiment(
		"token=3skip1_error=5%_indexchunks=false",
		threeSkip1,
		false,
		fivePctError,
	),

	NewExperiment(
		"token=3skip2_error=5%_indexchunks=true",
		threeSkip2,
		true,
		fivePctError,
	),
	NewExperiment(
		"token=3skip2_error=5%_indexchunks=false",
		threeSkip2,
		false,
		fivePctError,
	),
}

func analyze(metrics *Metrics, sampler Sampler, shipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string) error {
	metrics.tenants.Add(float64(len(tenants)))

	var n int         // count iterated series
	reportEvery := 10 // report every n chunks
	pool := newPool(runtime.NumCPU())

	for _, tenant := range tenants {
		err := shipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			indexshipper_index.ForEachIndexCallback(func(isMultiTenantIndex bool, idx indexshipper_index.Index) error {
				if isMultiTenantIndex {
					return nil
				}

				casted := idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex)
				_ = casted.ForSeries(
					context.Background(),
					nil, model.Earliest, model.Latest,
					func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
						chksCpy := make([]index.ChunkMeta, len(chks))
						copy(chksCpy, chks)
						pool.acquire(
							ls.Copy(),
							fp,
							chksCpy,
							func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {

								metrics.series.Inc()
								metrics.chunks.Add(float64(len(chks)))

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

								// record raw chunk sizes
								var chunkTotalUncompressedSize int
								for _, c := range got {
									chunkTotalUncompressedSize += c.Data.(*chunkenc.Facade).LokiChunk().UncompressedSize()
								}
								metrics.chunkSize.Observe(float64(chunkTotalUncompressedSize))
								n += len(got)

								// iterate experiments
								for experimentIdx, experiment := range experiments {

									sbf := experiment.bloom()

									// Iterate chunks
									var (
										lines, inserts, collisions float64
									)
									for idx := range got {
										tokenizer := experiment.tokenizer
										if experiment.encodeChunkID {
											tokenizer = ChunkIDTokenizer(got[idx].ChunkRef, tokenizer)
										}
										lc := got[idx].Data.(*chunkenc.Facade).LokiChunk()

										// Only report on the last experiment since they run serially
										if experimentIdx == len(experiments)-1 && (n+idx+1)%reportEvery == 0 {
											estimatedProgress := float64(fp) / float64(model.Fingerprint(math.MaxUint64)) * 100.
											level.Info(util_log.Logger).Log(
												"msg", "iterated",
												"progress", fmt.Sprintf("%.2f%%", estimatedProgress),
												"chunks", len(chks),
												"series", ls.String(),
											)
										}

										itr, err := lc.Iterator(
											context.Background(),
											time.Unix(0, 0),
											time.Unix(0, math.MaxInt64),
											logproto.FORWARD,
											log.NewNoopPipeline().ForStream(ls),
										)
										helpers.ExitErr("getting iterator", err)

										for itr.Next() && itr.Error() == nil {
											toks := tokenizer.Tokens(itr.Entry().Line)
											lines++
											for _, tok := range toks {
												for _, str := range []string{tok.Key, tok.Value} {
													if str != "" {
														if dup := sbf.TestAndAdd([]byte(str)); dup {
															collisions++
														}
														inserts++
													}
												}
											}
										}
										helpers.ExitErr("iterating chunks", itr.Error())
									}

									metrics.bloomSize.WithLabelValues(experiment.name).Observe(float64(sbf.Capacity() / 8))
									fillRatio := sbf.FillRatio()
									metrics.hammingWeightRatio.WithLabelValues(experiment.name).Observe(fillRatio)
									metrics.estimatedCount.WithLabelValues(experiment.name).Observe(
										float64(estimatedCount(sbf.Capacity(), sbf.FillRatio())),
									)
									metrics.lines.WithLabelValues(experiment.name).Add(lines)
									metrics.inserts.WithLabelValues(experiment.name).Add(inserts)
									metrics.collisions.WithLabelValues(experiment.name).Add(collisions)

								}

								metrics.seriesKept.Inc()
								metrics.chunksKept.Add(float64(len(chks)))
								metrics.chunksPerSeries.Observe(float64(len(chks)))

							},
						)

					},
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				return nil

			}),
		)
		helpers.ExitErr(fmt.Sprintf("iterating tenant %s", tenant), err)

	}

	level.Info(util_log.Logger).Log("msg", "waiting for workers to finish")
	pool.drain() // wait for workers to finishh
	level.Info(util_log.Logger).Log("msg", "waiting for final scrape")
	time.Sleep(30 * time.Second) // allow final scrape
	return nil
}

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}
