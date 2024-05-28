package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type localTSDBFile struct {
	path string
}

func (i localTSDBFile) Name() string { return i.path }
func (i localTSDBFile) Path() string { return i.path }

func main() {

	tenant := flag.String("tenant", "", "Source tenant identifier, default is `fake` for single tenant Loki")
	file := flag.String("file", "", "Source TSDB index file")
	query := flag.String("query", "", "Label matchers")

	flag.Parse()
	fmt.Println("-tenant=", *tenant, "-file=", *file, "-query=", *query)

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{From: config.NewDayTime(0), Schema: "v13"},
		},
	}

	// nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	// if err != nil {
	// 	log.Fatal("Failed to create label matcher:", err)
	// }

	matchers := []*labels.Matcher{}

	if *query != "" {
		m, err := syntax.ParseMatchers(*query, true)
		if err != nil {
			log.Fatal("Failed to parse log matcher:", err)
		}
		matchers = append(matchers, m...)
	}

	var (
		seriesRes []tsdb.Series
		chunkRes  []tsdb.ChunkRef
	)

	casted, err := tsdb.NewShippableTSDBFile(localTSDBFile{path: *file})
	if err != nil {
		log.Fatal(err)
	}
	from, through := casted.Bounds()

	fmt.Println("TSDB:", from.Time().String(), through.Time().String())
	fmt.Println("")

	seriesRes, err = casted.Series(
		context.Background(),
		*tenant,
		model.Earliest,
		model.Latest,
		seriesRes,
		nil,
		matchers...,
	)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("found series")
	for i := range seriesRes {
		fmt.Println(seriesRes[i].Fingerprint, seriesRes[i].Labels.String())
	}
	fmt.Println("")

	chunkRes, err = casted.GetChunkRefs(
		context.Background(),
		*tenant,
		model.Earliest,
		model.Latest,
		chunkRes,
		nil,
		matchers...,
	)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("found chunk refs")
	for i := range chunkRes {
		fmt.Println(chunkRes[i].Fingerprint, chunkRes[i].Start, chunkRes[i].End)
	}
	fmt.Println("")

	err = casted.Index.(*tsdb.TSDBIndex).ForSeries(
		context.Background(),
		"", nil,
		model.Earliest,
		model.Latest,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
			fmt.Println("fp", fp)
			for _, chk := range chks {
				key := s.ExternalKey(logproto.ChunkRef{
					Fingerprint: uint64(fp),
					UserID:      *tenant,
					From:        chk.From(),
					Through:     chk.Through(),
					Checksum:    chk.Checksum,
				})
				fmt.Println("chunk", key, chk.Entries, "entries")
			}
			return false
		},
		matchers...,
	)

	if err != nil {
		log.Fatal(err)
	}

}
