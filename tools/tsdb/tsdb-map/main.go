package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"strconv"

	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/config"
	boltdbcompactor "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/boltdb/compactor"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
)

var (
	source = flag.String("source", "", "the source boltdb file")
	dest   = flag.String("dest", "", "the dest tsdb dir")
	// Hardcode a periodconfig for convenience as the boltdb iterator needs one
	// NB: must match the index file you're reading from
	periodConfig = func() config.PeriodConfig {
		input := `
from: "2022-01-01"
index:
  period: 24h
  prefix: loki_index_
object_store: gcs
schema: v13
store: boltdb-shipper
`
		var cfg config.PeriodConfig
		if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
			panic(err)
		}
		return cfg
	}()
)

func extractChecksumFromChunkID(b []byte) uint32 {
	i := bytes.LastIndexByte(b, ':')
	x, err := strconv.ParseUint(string(b[i+1:]), 16, 32)
	if err != nil {
		panic(err)
	}
	return uint32(x)
}

func main() {
	flag.Parse()

	if source == nil || *source == "" {
		panic("source is required")
	}

	if dest == nil || *dest == "" {
		panic("dest is required")
	}

	db, err := util.SafeOpenBoltdbFile(*source)
	if err != nil {
		panic(err)
	}

	indexFormat, err := periodConfig.TSDBFormat()
	if err != nil {
		panic(err)
	}

	builder := tsdb.NewBuilder(indexFormat)

	log.Println("Loading index into memory")

	// loads everything into memory.
	if err := db.View(func(t *bbolt.Tx) error {
		return boltdbcompactor.ForEachChunk(context.Background(), t.Bucket([]byte("index")), periodConfig, func(entry retention.ChunkEntry) (bool, error) {
			builder.AddSeries(entry.Labels, model.Fingerprint(entry.Labels.Hash()), []index.ChunkMeta{{
				Checksum: extractChecksumFromChunkID(entry.ChunkID),
				MinTime:  int64(entry.From),
				MaxTime:  int64(entry.Through),
				KB:       ((3 << 20) / 4) / 1024, // guess: 0.75mb, 1/2 of the max size, rounded to KB
				Entries:  10000,                  // guess: 10k entries
			}})
			return false, nil
		})
	}); err != nil {
		panic(err)
	}

	log.Println("writing index")
	if _, err := builder.Build(context.Background(), *dest, func(_, _ model.Time, _ uint32) tsdb.Identifier {
		panic("todo")
	}); err != nil {
		panic(err)
	}
}
