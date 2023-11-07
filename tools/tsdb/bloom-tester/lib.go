package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"

	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"

	"hash/fnv"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	bt "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
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

	periodCfg, tableRange, tableName, err := helpers.GetPeriodConfigForTableNumber(bucket, conf.SchemaConfig.Configs)
	helpers.ExitErr("find period config for bucket", err)

	objectClient, err := storage.NewObjectClient(periodCfg.ObjectType, conf.StorageConfig, clientMetrics)
	helpers.ExitErr("creating object client", err)

	chunkClient := client.NewClient(objectClient, nil, conf.SchemaConfig)

	openFn := func(p string) (shipperindex.Index, error) {
		return tsdb.OpenShippableTSDB(p)
	}

	indexShipper, err := indexshipper.NewIndexShipper(
		periodCfg.IndexTables.PathPrefix,
		conf.StorageConfig.TSDBShipperConfig,
		objectClient,
		overrides,
		nil,
		openFn,
		tableRange,
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", prometheus.DefaultRegisterer),
		util_log.Logger,
	)
	helpers.ExitErr("creating index shipper", err)

	tenants, err := helpers.ResolveTenants(objectClient, periodCfg.IndexTables.PathPrefix, tableName)
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
	three      = bt.NewNGramTokenizer(3, 4, 0)
	threeSkip1 = bt.NewNGramTokenizer(3, 4, 1)
	threeSkip2 = bt.NewNGramTokenizer(3, 4, 2)
	threeSkip3 = bt.NewNGramTokenizer(3, 4, 3)
	four       = bt.NewNGramTokenizer(4, 5, 0)
	fourSkip1  = bt.NewNGramTokenizer(4, 5, 1)
	fourSkip2  = bt.NewNGramTokenizer(4, 5, 2)
	five       = bt.NewNGramTokenizer(5, 6, 0)
	six        = bt.NewNGramTokenizer(6, 7, 0)

	onePctError  = func() *filter.ScalableBloomFilter { return filter.NewScalableBloomFilter(1024, 0.01, 0.8) }
	fivePctError = func() *filter.ScalableBloomFilter { return filter.NewScalableBloomFilter(1024, 0.05, 0.8) }
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
	/*
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
		),*/
	/*
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

	var n int // count iterated series
	//pool := newPool(runtime.NumCPU())
	//pool := newPool(1)
	bloomTokenizer, _ := bt.NewBloomTokenizer(prometheus.DefaultRegisterer)
	for _, tenant := range tenants {
		level.Info(util_log.Logger).Log("Analyzing tenant", tenant, "table", tableName)
		err := indexShipper.ForEach(
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
								n += len(got)

								// iterate experiments
								for _, experiment := range experiments {
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
										bloomTokenizer.SetLineTokenizer(experiment.tokenizer)

										level.Info(util_log.Logger).Log("Starting work on: ", ls.String(), "'", FNV32a(ls.String()), "'", experiment.name, tenant)
										startTime := time.Now().UnixMilli()

										sbf := experiment.bloom()
										bloom := bt.Bloom{
											ScalableBloomFilter: *sbf,
										}
										series := bt.Series{
											Fingerprint: fp,
										}
										swb := bt.SeriesWithBloom{
											Bloom:  &bloom,
											Series: &series,
										}
										bloomTokenizer.PopulateSeriesWithBloom(&swb, got)

										endTime := time.Now().UnixMilli()
										if len(got) > 0 {
											metrics.bloomSize.WithLabelValues(experiment.name).Observe(float64(sbf.Capacity() / 8))
											fillRatio := sbf.FillRatio()
											metrics.hammingWeightRatio.WithLabelValues(experiment.name).Observe(fillRatio)
											metrics.estimatedCount.WithLabelValues(experiment.name).Observe(
												float64(estimatedCount(sbf.Capacity(), sbf.FillRatio())),
											)

											writeSBF(&swb.Bloom.ScalableBloomFilter,
												os.Getenv("DIR"),
												fmt.Sprint(bucketPrefix, experiment.name),
												os.Getenv("BUCKET"),
												tenant,
												ls.String(),
												objectClient)

											metrics.sbfCreationTime.WithLabelValues(experiment.name).Add(float64(endTime - startTime))
											metrics.sbfsCreated.WithLabelValues(experiment.name).Inc()
											metrics.chunkSize.Observe(float64(chunkTotalUncompressedSize))

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

			},
		)
		helpers.ExitErr(fmt.Sprintf("iterating tenant %s", tenant), err)

	}

	level.Info(util_log.Logger).Log("msg", "waiting for workers to finish")
	//pool.drain() // wait for workers to finish
	level.Info(util_log.Logger).Log("msg", "waiting for final scrape")
	//time.Sleep(30 * time.Second)         // allow final scrape
	time.Sleep(time.Duration(1<<63 - 1)) // wait forever
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

func writeSBF(sbf *filter.ScalableBloomFilter, location, prefix, period, tenant, series string, objectClient client.ObjectClient) {
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

func writeSBFToFile(sbf *filter.ScalableBloomFilter, filename string) error {
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

func writeSBFToObjectStorage(_ *filter.ScalableBloomFilter, objectStorageFilename, localFilename string, objectClient client.ObjectClient) {
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
