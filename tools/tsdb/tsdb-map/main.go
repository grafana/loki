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

	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
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

	db, err := shipper_util.SafeOpenBoltdbFile(*source)
	if err != nil {
		panic(err)
	}

	builder := tsdb.NewBuilder()

	log.Println("Loading index into memory")

	// loads everything into memory.
	if err := db.View(func(t *bbolt.Tx) error {
		it, err := retention.NewChunkIndexIterator(t.Bucket([]byte("index")), periodConfig)
		if err != nil {
			return err
		}

		for it.Next() {
			if it.Err() != nil {
				return it.Err()
			}
			entry := it.Entry()
			builder.AddSeries(entry.Labels, model.Fingerprint(entry.Labels.Hash()), []index.ChunkMeta{{
				Checksum: extractChecksumFromChunkID(entry.ChunkID),
				MinTime:  int64(entry.From),
				MaxTime:  int64(entry.Through),
				KB:       ((3 << 20) / 4) / 1024, // guess: 0.75mb, 1/2 of the max size, rounded to KB
				Entries:  10000,                  // guess: 10k entries
			}})
		}

		return nil
	}); err != nil {
		panic(err)
	}

	log.Println("writing index")
	if _, err := builder.Build(context.Background(), *dest, func(from, through model.Time, checksum uint32) tsdb.Identifier {
		panic("todo")
	}); err != nil {
		panic(err)
	}
}
