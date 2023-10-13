package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"

	//"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"hash/fnv"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/owen-d/BoomFilters/boom"
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
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
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

	//sampler, err := NewProbabilisticSampler(0.00008)
	sampler, err := NewProbabilisticSampler(1.000)
	helpers.ExitErr("creating sampler", err)

	metrics := NewMetrics(prometheus.DefaultRegisterer)

	level.Info(util_log.Logger).Log("msg", "starting server")
	err = services.StartAndAwaitRunning(context.Background(), svc)
	helpers.ExitErr("waiting for service to start", err)
	level.Info(util_log.Logger).Log("msg", "server started")

	err = analyze(metrics, sampler, indexShipper, chunkClient, tableName, tenants, objectClient)
	helpers.ExitErr("analyzing", err)
}

var (
	three      = newNGramTokenizer(3, 4, 0)
	threeSkip1 = newNGramTokenizer(3, 4, 1)
	threeSkip2 = newNGramTokenizer(3, 4, 2)
	threeSkip3 = newNGramTokenizer(3, 4, 3)
	four       = newNGramTokenizer(4, 5, 0)
	fourSkip1  = newNGramTokenizer(4, 5, 1)
	fourSkip2  = newNGramTokenizer(4, 5, 2)
	five       = newNGramTokenizer(5, 6, 0)
	six        = newNGramTokenizer(6, 7, 0)

	onePctError  = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.01, 0.8) }
	fivePctError = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.05, 0.8) }
)

var experiments = []Experiment{
	// n > error > skip > index

	/*
		NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		),
	*/
	NewExperiment(
		"token=4skip0_error=1%_indexchunks=true",
		four,
		true,
		onePctError,
	),
	NewExperiment(
		"token=4skip1_error=1%_indexchunks=true",
		fourSkip1,
		true,
		onePctError,
	),
	NewExperiment(
		"token=4skip2_error=1%_indexchunks=true",
		fourSkip2,
		true,
		onePctError,
	),
	NewExperiment(
		"token=4skip0_error=5%_indexchunks=true",
		four,
		true,
		fivePctError,
	),
	NewExperiment(
		"token=4skip1_error=5%_indexchunks=true",
		fourSkip1,
		true,
		fivePctError,
	),
	NewExperiment(
		"token=4skip2_error=5%_indexchunks=true",
		fourSkip2,
		true,
		fivePctError,
	),
	/*
		NewExperiment(
			"token=5skip0_error=1%_indexchunks=true",
			five,
			true,
			onePctError,
		),
		NewExperiment(
			"token=6skip0_error=1%_indexchunks=true",
			six,
			true,
			onePctError,
		),
	*/
	/*
		NewExperiment(
			"token=3skip0_error=1%_indexchunks=false",
			three,
			false,
			onePctError,
		),
	*/
	/*
		NewExperiment(
			"token=3skip1_error=1%_indexchunks=true",
			threeSkip1,
			true,
			onePctError,
		),*/
	/*
		NewExperiment(
			"token=3skip1_error=1%_indexchunks=false",
			threeSkip1,
			false,
			onePctError,
		),
	*/
	/*
		NewExperiment(
			"token=3skip2_error=1%_indexchunks=true",
			threeSkip2,
			true,
			onePctError,
		),*/
	/*
		NewExperiment(
			"token=3skip2_error=1%_indexchunks=false",
			threeSkip2,
			false,
			onePctError,
		),
	*/
	/*
		NewExperiment(
			"token=3skip0_error=5%_indexchunks=true",
			three,
			true,
			fivePctError,
		),*/
	/*
		NewExperiment(
			"token=3skip0_error=5%_indexchunks=false",
			three,
			false,
			fivePctError,
		),
	*/
	/*
		NewExperiment(
			"token=3skip1_error=5%_indexchunks=true",
			threeSkip1,
			true,
			fivePctError,
		),*/
	/*
		NewExperiment(
			"token=3skip1_error=5%_indexchunks=false",
			threeSkip1,
			false,
			fivePctError,
		),
	*/
	/*
		NewExperiment(
			"token=3skip2_error=5%_indexchunks=true",
			threeSkip2,
			true,
			fivePctError,
		),*/
	/*
		NewExperiment(
			"token=3skip2_error=5%_indexchunks=false",
			threeSkip2,
			false,
			fivePctError,
		),

	*/
}

func analyze(metrics *Metrics, sampler Sampler, indexShipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string, objectClient client.ObjectClient) error {
	metrics.tenants.Add(float64(len(tenants)))

	testerNumber := extractTesterNumber(os.Getenv("HOSTNAME"))
	if testerNumber == -1 {
		helpers.ExitErr("extracting hostname index number", nil)
	}
	numTesters, _ := strconv.Atoi(os.Getenv("NUM_TESTERS"))
	if numTesters == -1 {
		helpers.ExitErr("extracting total number of testers", nil)
	}
	level.Info(util_log.Logger).Log("msg", "starting analyze()", "tester", testerNumber, "total", numTesters)

	var n int         // count iterated series
	reportEvery := 10 // report every n chunks
	//pool := newPool(runtime.NumCPU())
	//pool := newPool(1)

	for _, tenant := range tenants {
		level.Info(util_log.Logger).Log("Analyzing tenant", tenant, "table", tableName)
		err := indexShipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			shipperindex.ForEachIndexCallback(func(isMultiTenantIndex bool, idx shipperindex.Index) error {
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

						if (workernumber == testerNumber) && (len(chks) < 10000) { // for each series

							/*(pool.acquire(
							ls.Copy(),
							fp,
							chksCpy,
							func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {*/

							metrics.series.Inc()
							metrics.chunks.Add(float64(len(chks)))

							if !sampler.Sample() {
								return
							}

							cache := NewLRUCache4(150000)

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
								// record raw chunk sizes
								var chunkTotalUncompressedSize int
								for _, c := range got {
									chunkTotalUncompressedSize += c.Data.(*chunkenc.Facade).LokiChunk().UncompressedSize()
								}
								metrics.chunkSize.Observe(float64(chunkTotalUncompressedSize))
								n += len(got)

								// iterate experiments
								for experimentIdx, experiment := range experiments {
									bucketPrefix := os.Getenv("BUCKET_PREFIX")
									if strings.EqualFold(bucketPrefix, "") {
										bucketPrefix = "named-experiments-"
									}
									if !sbfFileExists("bloomtests",
										fmt.Sprint(bucketPrefix, experiment.name),
										os.Getenv("BUCKET"),
										tenant,
										ls.String(),
										objectClient) {

										level.Info(util_log.Logger).Log("Starting work on: ", ls.String(), "'", FNV32a(ls.String()), "'", experiment.name, tenant)
										startTime := time.Now().UnixMilli()

										sbf := experiment.bloom()
										chunkTokenizer := ChunkIDTokenizerHalfInit(experiment.tokenizer)
										cache.Clear()

										// Iterate chunks
										var (
											lines, inserts, collisions float64
										)
										for cidx := range got {
											chunkTokenizer.reinit(got[cidx].ChunkRef)
											var tokenizer Tokenizer = chunkTokenizer
											if !experiment.encodeChunkID {
												tokenizer = experiment.tokenizer // so I don't have to change the lines of code below
											}
											lc := got[cidx].Data.(*chunkenc.Facade).LokiChunk()

											// Only report on the last experiment since they run serially
											if experimentIdx == len(experiments)-1 && (n+cidx+1)%reportEvery == 0 {
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
													if tok.Key != nil {
														if !cache.Get(tok.Key) {
															cache.Put(tok.Key)
															if dup := sbf.TestAndAdd(tok.Key); dup {
																collisions++
															}
															inserts++
														}
													}
												}
											}
											helpers.ExitErr("iterating chunks", itr.Error())
										} // for each chunk

										endTime := time.Now().UnixMilli()
										if len(got) > 0 {
											metrics.bloomSize.WithLabelValues(experiment.name).Observe(float64(sbf.Capacity() / 8))
											fillRatio := sbf.FillRatio()
											metrics.hammingWeightRatio.WithLabelValues(experiment.name).Observe(fillRatio)
											metrics.estimatedCount.WithLabelValues(experiment.name).Observe(
												float64(estimatedCount(sbf.Capacity(), sbf.FillRatio())),
											)
											metrics.lines.WithLabelValues(experiment.name).Add(lines)
											metrics.inserts.WithLabelValues(experiment.name).Add(inserts)
											metrics.collisions.WithLabelValues(experiment.name).Add(collisions)

											writeSBF(sbf,
												os.Getenv("DIR"),
												fmt.Sprint(bucketPrefix, experiment.name),
												os.Getenv("BUCKET"),
												tenant,
												ls.String(),
												objectClient)

											metrics.sbfCreationTime.WithLabelValues(experiment.name).Add(float64(endTime - startTime))
											metrics.sbfsCreated.WithLabelValues(experiment.name).Inc()

											if err != nil {
												helpers.ExitErr("writing sbf to file", err)
											}
										} // logging chunk stats block
									} // if sbf doesn't exist
								} // for each experiment
							} else {
								level.Info(util_log.Logger).Log("error getting chunks", err)
							}

							metrics.seriesKept.Inc()
							metrics.chunksKept.Add(float64(len(chks)))
							metrics.chunksPerSeries.Observe(float64(len(chks)))

							/*},
							)*/
						} // for each series

					},
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				return nil

			}),
		)
		helpers.ExitErr(fmt.Sprintf("iterating tenant %s", tenant), err)

	}

	level.Info(util_log.Logger).Log("msg", "waiting for workers to finish")
	//pool.drain() // wait for workers to finish
	level.Info(util_log.Logger).Log("msg", "waiting for final scrape")
	time.Sleep(30 * time.Second) // allow final scrape
	return nil
}

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}

func extractTesterNumber(input string) int {
	// Split the input string by '-' to get individual parts
	parts := strings.Split(input, "-")

	// Extract the last part (the number)
	lastPart := parts[len(parts)-1]

	// Attempt to convert the last part to an integer
	extractedNumber, err := strconv.Atoi(lastPart)
	if err != nil {
		return -1
	}

	// Send the extracted number to the result channel
	return extractedNumber
}

func AssignToWorker(index int, numWorkers int) int {
	// Calculate the hash of the index
	h := fnv.New32a()
	h.Write([]byte(fmt.Sprintf("%d", index)))
	hash := int(h.Sum32())

	// Use modulo to determine which worker should handle the index
	workerID := hash % numWorkers

	return workerID
}

func FNV32a(text string) string {
	hashAlgorithm := fnv.New32a()
	hashAlgorithm.Reset()
	hashAlgorithm.Write([]byte(text))
	return strconv.Itoa(int(hashAlgorithm.Sum32()))
}

func sbfFileExists(location, prefix, period, tenant, series string, objectClient client.ObjectClient) bool {
	dirPath := fmt.Sprintf("%s/%s/%s/%s", location, prefix, period, tenant)
	fullPath := fmt.Sprintf("%s/%s", dirPath, FNV32a(series))

	result, _ := objectClient.ObjectExists(context.Background(), fullPath)
	//fmt.Println(fullPath, result)
	return result
}

func writeSBF(sbf *boom.ScalableBloomFilter, location, prefix, period, tenant, series string, objectClient client.ObjectClient) {
	dirPath := fmt.Sprintf("%s/%s/%s/%s", location, prefix, period, tenant)
	objectStoragePath := fmt.Sprintf("bloomtests/%s/%s/%s", prefix, period, tenant)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		helpers.ExitErr("error creating sbf dir", err)
	}

	err := writeSBFToFile(sbf, fmt.Sprintf("%s/%s", dirPath, FNV32a(series)))
	if err != nil {
		helpers.ExitErr("writing sbf to file", err)
	}

	writeSBFToObjectStorage(sbf,
		fmt.Sprintf("%s/%s", objectStoragePath, FNV32a(series)),
		fmt.Sprintf("%s/%s", dirPath, FNV32a(series)),
		objectClient)
}

func writeSBFToFile(sbf *boom.ScalableBloomFilter, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	bytesWritten, err := sbf.WriteTo(w)
	if err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "wrote sbf", "bytes", bytesWritten, "file", filename)

	err = w.Flush()
	return err
}

func writeSBFToObjectStorage(sbf *boom.ScalableBloomFilter, objectStorageFilename, localFilename string, objectClient client.ObjectClient) {
	// Probably a better way to do this than to reopen the file, but it's late
	file, err := os.Open(localFilename)
	if err != nil {
		level.Info(util_log.Logger).Log("error opening", localFilename, "error", err)
	}

	defer file.Close()

	fileInfo, _ := file.Stat()
	var size = fileInfo.Size()

	buffer := make([]byte, size)

	// read file content to buffer
	_, _ = file.Read(buffer)

	fileBytes := bytes.NewReader(buffer) // converted to io.ReadSeeker type

	_ = objectClient.PutObject(context.Background(), objectStorageFilename, fileBytes)
	level.Info(util_log.Logger).Log("done writing", objectStorageFilename)
}
