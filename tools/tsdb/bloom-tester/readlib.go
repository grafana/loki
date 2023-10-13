package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/config"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"

	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client"

	//indexshipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"

	//"github.com/grafana/loki/pkg/storage/stores/tsdb"
	//"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/tools/tsdb/helpers"
)

var queryExperiments = []QueryExperiment{
	//NewQueryExperiment("three_char_word", "tra"),
	//NewQueryExperiment("four_char_word", "trac"),
	NewQueryExperiment("five_char_word", "trace"),
	NewQueryExperiment("six_char_word", "traceI"),
	NewQueryExperiment("seven_char_word", "traceID"),
	NewQueryExperiment("uuid", "2b1a5e46-36a2-4694-a4b1-f34cc7bdfc45"),
	NewQueryExperiment("longer_string_that_exists", "synthetic-monitoring-agent"),
	NewQueryExperiment("longer_string_that_doesnt_exist", "abcdefghjiklmnopqrstuvwxyzzy1234567890"),
}

func executeRead() {
	conf, svc, bucket, err := helpers.Setup()
	helpers.ExitErr("setting up", err)

	_, overrides, clientMetrics := helpers.DefaultConfigs()

	flag.Parse()

	objectClient, err := storage.NewObjectClient(conf.StorageConfig.TSDBShipperConfig.SharedStoreType, conf.StorageConfig, clientMetrics)
	helpers.ExitErr("creating object client", err)

	chunkClient := client.NewClient(objectClient, nil, conf.SchemaConfig)

	tableRanges := helpers.GetIndexStoreTableRanges(config.TSDBType, conf.SchemaConfig.Configs)

	openFn := func(p string) (shipperindex.Index, error) {
		return tsdb.OpenShippableTSDB(p, tsdb.IndexOpts{})
	}

	indexShipper, err := indexshipper.NewIndexShipper(
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
	level.Info(util_log.Logger).Log("tenants", strings.Join(tenants, ","), "table", tableName)
	helpers.ExitErr("resolving tenants", err)

	sampler, err := NewProbabilisticSampler(1.000)
	helpers.ExitErr("creating sampler", err)

	metrics := NewMetrics(prometheus.DefaultRegisterer)

	level.Info(util_log.Logger).Log("msg", "starting server")
	err = services.StartAndAwaitRunning(context.Background(), svc)
	helpers.ExitErr("waiting for service to start", err)
	level.Info(util_log.Logger).Log("msg", "server started")

	err = analyzeRead(metrics, sampler, indexShipper, chunkClient, tableName, tenants, objectClient)
	helpers.ExitErr("analyzing", err)
}

func analyzeRead(metrics *Metrics, sampler Sampler, shipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string, objectClient client.ObjectClient) error {
	metrics.readTenants.Add(float64(len(tenants)))

	testerNumber := extractTesterNumber(os.Getenv("HOSTNAME"))
	if testerNumber == -1 {
		helpers.ExitErr("extracting hostname index number", nil)
	}
	numTesters, _ := strconv.Atoi(os.Getenv("NUM_TESTERS"))
	if numTesters == -1 {
		helpers.ExitErr("extracting total number of testers", nil)
	}
	level.Info(util_log.Logger).Log("msg", "starting analyze()", "tester", testerNumber, "total", numTesters)

	//var n int // count iterated series
	//reportEvery := 10 // report every n chunks
	//pool := newPool(runtime.NumCPU())
	//pool := newPool(16)
	//searchString := os.Getenv("SEARCH_STRING")
	//147854,148226,145541,145603,147159,147836,145551,145599,147393,147841,145265,145620,146181,147225,147167,146131,146189,146739,147510,145572,146710,148031,29,146205,147175,146984,147345
	//mytenants := []string{"29"}
	for _, tenant := range tenants {
		level.Info(util_log.Logger).Log("Analyzing tenant", tenant, "table", tableName)
		err := shipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			func(isMultiTenantIndex bool, idx shipperindex.Index) error {
				if isMultiTenantIndex {
					return nil
				}

				casted := idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex)
				_ = casted.ForSeries(
					context.Background(),
					nil, model.Earliest, model.Latest,
					func(ls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) {
						seriesString := ls.String()
						seriesStringHash := FNV32a(seriesString)
						pos, _ := strconv.Atoi(seriesStringHash)

						workernumber := AssignToWorker(pos, numTesters)

						//fmt.Println("num workers", numTesters)
						//fmt.Println("seriesSTring", seriesString)
						//fmt.Println("seriesSTringHasj", seriesStringHash)
						//fmt.Println("pos", pos)
						fmt.Println("workernumber", workernumber)

						if (workernumber == testerNumber) && (len(chks) < 10000) { // For every series
							/*
								pool.acquire(
									ls.Copy(),
									fp,
									chksCpy,
									func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {*/

							metrics.readSeries.Inc()
							metrics.readChunks.Add(float64(len(chks)))

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
							if err == nil {
								bucketPrefix := os.Getenv("BUCKET_PREFIX")
								if strings.EqualFold(bucketPrefix, "") {
									bucketPrefix = "named-experiments-"
								}
								for _, experiment := range experiments { // for each experiment
									if sbfFileExists("bloomtests",
										fmt.Sprint(bucketPrefix, experiment.name),
										os.Getenv("BUCKET"),
										tenant,
										ls.String(),
										objectClient) {

										sbf := readSBFFromObjectStorage("bloomtests",
											fmt.Sprint(bucketPrefix, experiment.name),
											os.Getenv("BUCKET"),
											tenant,
											ls.String(),
											objectClient)
										for gotIdx := range got { // for every chunk
											for _, queryExperiment := range queryExperiments { // for each search string
												if len(queryExperiment.searchString) >= experiment.tokenizer.getMin() {

													foundInChunk := false
													foundInSbf := false

													chunkTokenizer := ChunkIDTokenizerHalfInit(experiment.tokenizer)

													chunkTokenizer.reinit(got[gotIdx].ChunkRef)
													var tokenizer Tokenizer = chunkTokenizer
													if !experiment.encodeChunkID {
														tokenizer = experiment.tokenizer
													}

													for i := 0; i <= tokenizer.getSkip(); i++ {
														numMatches := 0
														if (len(queryExperiment.searchString) - i) >= tokenizer.getMin() {
															tokens := tokenizer.Tokens(queryExperiment.searchString[i:])

															for _, token := range tokens {
																if sbf.Test(token.Key) {
																	numMatches++
																}
															}
															if numMatches > 0 {
																if numMatches == len(tokens) {
																	foundInSbf = true
																	metrics.sbfMatchesPerSeries.WithLabelValues(experiment.name, queryExperiment.name).Inc()
																}
															}
														}
													}

													lc := got[gotIdx].Data.(*chunkenc.Facade).LokiChunk()

													itr, err := lc.Iterator(
														context.Background(),
														time.Unix(0, 0),
														time.Unix(0, math.MaxInt64),
														logproto.FORWARD,
														log.NewNoopPipeline().ForStream(ls),
													)
													helpers.ExitErr("getting iterator", err)

													for itr.Next() && itr.Error() == nil {
														if strings.Contains(itr.Entry().Line, queryExperiment.searchString) {
															//fmt.Println("Line match: ", itr.Entry().Line)
															foundInChunk = true
														}
													}

													if foundInChunk {
														if foundInSbf {
															//fmt.Println("true positive", experiment.name, queryExperiment.name, gotIdx)
															metrics.sbfLookups.WithLabelValues(experiment.name, queryExperiment.name, TruePositive).Inc()
														} else {
															level.Info(util_log.Logger).Log("**** false negative", experiment.name, queryExperiment.name, ls.String(), gotIdx, testerNumber)
															metrics.sbfLookups.WithLabelValues(experiment.name, queryExperiment.name, FalseNegative).Inc()
														}
													} else {
														if foundInSbf {
															metrics.sbfLookups.WithLabelValues(experiment.name, queryExperiment.name, FalsePositive).Inc()
															//fmt.Println("false positive", experiment.name, queryExperiment.name, gotIdx, ls.String())
														} else {
															metrics.sbfLookups.WithLabelValues(experiment.name, queryExperiment.name, TrueNegative).Inc()
															//fmt.Println("true negative", experiment.name, queryExperiment.name, gotIdx)
														}
													}

													metrics.experimentCount.Inc()

													helpers.ExitErr("iterating chunks ", itr.Error())
												}
												/*else // if search string is long enough
												{
													//	fmt.Println("Skipping", queryExperiment.name, "because it's too short", experiment.name)
												}*/

											} // for each search string
										} // for every chunk

										metrics.sbfCount.Inc()
										metrics.readBloomSize.WithLabelValues(experiment.name).Observe(float64(sbf.Capacity() / 8))

									} // for existing sbf files
								} // for every experiment

							} else {
								level.Info(util_log.Logger).Log("error getting chunks", err)
							}
							if len(got) > 0 { // we have chunks, record size info
								var chunkTotalUncompressedSize int
								for _, c := range got {
									chunkTotalUncompressedSize += c.Data.(*chunkenc.Facade).LokiChunk().UncompressedSize()
								}
								metrics.readChunkSize.Observe(float64(chunkTotalUncompressedSize))
								metrics.readChunksKept.Add(float64(len(chks)))
							}

							metrics.readSeriesKept.Inc()
							/*
									},
								)
							*/
						} // For every series
					},
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				return nil

			},
		)
		helpers.ExitErr(fmt.Sprintf("iterating tenant %s", tenant), err)

	}

	level.Info(util_log.Logger).Log("msg", "waiting for workers to finish")
	//pool.drain() // wait for workers to finishh
	level.Info(util_log.Logger).Log("msg", "waiting for final scrape")
	time.Sleep(30 * time.Second)         // allow final scrape
	time.Sleep(time.Duration(1<<63 - 1)) // wait forever
	return nil
}

func readSBFFromObjectStorage(location, prefix, period, tenant, series string, objectClient client.ObjectClient) *filter.ScalableBloomFilter {
	objectStoragePath := fmt.Sprintf("%s/%s/%s/%s", location, prefix, period, tenant)

	sbf := experiments[0].bloom()
	closer, _, _ := objectClient.GetObject(context.Background(), fmt.Sprintf("%s/%s", objectStoragePath, FNV32a(series)))
	_, _ = sbf.ReadFrom(closer)
	return sbf
}
