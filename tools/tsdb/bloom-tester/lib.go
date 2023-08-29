package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"runtime"
	"time"
	"unicode/utf8"

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
	)
	helpers.ExitErr("creating index shipper", err)

	tenants, tableName, err := helpers.ResolveTenants(objectClient, bucket, tableRanges)
	helpers.ExitErr("resolving tenants", err)

	sampler, err := NewProbabilisticSampler(0.001)
	helpers.ExitErr("creating sampler", err)

	metrics := NewMetrics(prometheus.DefaultRegisterer)

	level.Info(util_log.Logger).Log("msg", "starting server")
	err = services.StartAndAwaitRunning(context.Background(), svc)
	helpers.ExitErr("waiting for service to start", err)
	level.Info(util_log.Logger).Log("msg", "server started")

	err = analyze(metrics, sampler, shipper, chunkClient, tableName, tenants)
	helpers.ExitErr("analyzing", err)
	level.Info(util_log.Logger).Log("msg", "finished analyzing")
}

func analyze(metrics *Metrics, sampler Sampler, shipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string) error {
	tokenizer := newNGramTokenizer(5, 6)
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
				casted.ForSeries(
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

								metrics.seriesKept.Inc()
								metrics.chunksKept.Add(float64(len(chks)))
								metrics.chunksPerSeries.Observe(float64(len(chks)))

								sbf := boom.NewScalableBloomFilter(1000, 0.01, 0.8)

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

								for idx := range got {
									lc := got[idx].Data.(*chunkenc.Facade).LokiChunk()

									if (n+idx+1)%reportEvery == 0 {
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
										for _, tok := range toks {
											if tok.Key != "" {
												if dup := sbf.TestAndAdd([]byte(tok.Key)); dup {
													metrics.collisions.Inc()
													metrics.keyCollisions.Inc()
												}
												metrics.inserts.Inc()
												metrics.keysInserted.Inc()
											}

											if tok.Value != "" {
												if dup := sbf.TestAndAdd([]byte(tok.Key)); dup {
													metrics.collisions.Inc()
													metrics.valueCollisions.Inc()
												}
												metrics.inserts.Inc()
												metrics.valuesInserted.Inc()
											}
										}
									}
								}

								helpers.ExitErr("iterating chunks", err)
								var chunkTotalUncompressedSize int
								for _, c := range got {
									chunkTotalUncompressedSize += c.Data.(*chunkenc.Facade).LokiChunk().UncompressedSize()
								}
								metrics.chunkSize.Observe(float64(chunkTotalUncompressedSize))
								n += len(got)

								metrics.bloomSize.Observe(float64(sbf.Capacity() / 8))
								metrics.hammingWeightRatio.Observe(sbf.FillRatio())
								// TODO: fillratio is implemented linearly, find a better way to estimate that isn't O(n)
								metrics.estimatedCount.Observe(float64(estimatedCount(sbf.Capacity(), sbf.FillRatio(), sbf.K())))
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
	return nil
}

type Token struct {
	// Either key or value may be empty
	Key, Value string
}
type Tokenizer interface {
	Tokens(line string) []Token
}

type logfmtTokenizer struct {
	parser *log.LogfmtParser
	lbls   *log.LabelsBuilder
}

func (t *logfmtTokenizer) Tokens(line string) []Token {
	t.lbls.Reset()
	t.parser.Process(0, []byte(line), t.lbls)
	ls := t.lbls.LabelsResult().Labels()
	res := make([]Token, 0, len(ls))
	for _, l := range ls {
		res = append(res, Token{Key: l.Name, Value: l.Value})
	}
	return res
}

func newLogfmtTokenizer() *logfmtTokenizer {
	return &logfmtTokenizer{
		// non strict, allow empty values
		parser: log.NewLogfmtParser(false, true),
		lbls:   log.NewBaseLabelsBuilder().ForLabels(nil, 0),
	}
}

type ngramTokenizer struct {
	// [min,max) exclusivity
	min, max int
	buffers  [][]rune // circular buffers used for ngram generation
}

func newNGramTokenizer(min, max int) *ngramTokenizer {
	t := &ngramTokenizer{
		min: min,
		max: max,
	}
	for i := t.min; i < t.max; i++ {
		t.buffers = append(t.buffers, make([]rune, i))
	}

	return t

}

func (t *ngramTokenizer) Tokens(line string) (res []Token) {
	for i, r := range line {
		// j is the index of the buffer to use
		for j := 0; j < (t.max - t.min); j++ {
			// n is the length of the ngram
			n := j + t.min
			// pos is the position in the buffer to overwrite
			pos := i % n
			t.buffers[j][pos] = r

			if i >= n-1 {
				ngram := reassemble(t.buffers[j], (i+1)%n)
				res = append(res, Token{Key: string(ngram), Value: ""})
			}
		}
	}
	return
}

func reassemble(buf []rune, pos int) []byte {
	res := make([]byte, 0, len(buf)*4) // 4 bytes per rune (i32)
	for i := 0; i < len(buf); i++ {
		cur := (pos + i) % len(buf)
		res = utf8.AppendRune(res, buf[cur])
	}
	return res
}

func estimatedCount(m uint, hammingRatio float64, k uint) int {
	return int(-float64(m) * math.Log(1-hammingRatio) / float64(k))
}
