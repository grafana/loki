// ingested-at-analyzer reports how many bytes of a TSDB index file are spent
// encoding the ChunkMeta.IngestedAt field introduced in index FormatV4
// (schema v14).
//
// Usage:
//
//	go run ./tools/tsdb/ingested-at-analyzer /path/to/index.tsdb[.gz] [...]
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s <index.tsdb[.gz]> [...]\n", os.Args[0])
		os.Exit(1)
	}

	for _, path := range flag.Args() {
		if err := analyze(path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

type stats struct {
	Version         int
	IndexSize       int64
	Series          int
	Chunks          int
	ChunksWithValue int
	IngestedAtBytes int
	SentinelBytes   int // chunks without an ingestion time still cost 1 byte each
}

func analyze(path string) error {
	path, cleanup, err := maybeGunzip(path)
	if err != nil {
		return err
	}
	defer cleanup()

	s, err := analyzeFile(path)
	if err != nil {
		return err
	}

	fmt.Printf("file: %s\n", path)
	fmt.Printf("index version: %d\n", s.Version)
	fmt.Printf("index size: %d bytes\n", s.IndexSize)

	if s.Version < index.FormatV4 {
		fmt.Println("index predates FormatV4: no ingestedAt field encoded")
		return nil
	}

	fmt.Printf("series: %d\n", s.Series)
	fmt.Printf("chunks: %d (%d with ingestedAt set)\n", s.Chunks, s.ChunksWithValue)
	fmt.Printf("ingestedAt bytes: %d (%.4f%% of index size)\n",
		s.IngestedAtBytes, 100*float64(s.IngestedAtBytes)/float64(s.IndexSize))
	fmt.Printf("  zero-sentinel bytes: %d\n", s.SentinelBytes)
	fmt.Printf("  non-zero value bytes: %d\n", s.IngestedAtBytes-s.SentinelBytes)
	return nil
}

func analyzeFile(path string) (stats, error) {
	reader, err := index.NewFileReader(path)
	if err != nil {
		return stats{}, err
	}
	defer reader.Close()

	s := stats{
		Version:   reader.Version(),
		IndexSize: reader.Size(),
	}
	if s.Version < index.FormatV4 {
		return s, nil
	}

	k, v := index.AllPostingsKey()
	postings, err := reader.Postings(k, nil, v)
	if err != nil {
		return stats{}, err
	}

	var chks []index.ChunkMeta
	for postings.Next() {
		if _, err := reader.Series(postings.At(), math.MinInt64, math.MaxInt64, nil, &chks); err != nil {
			return stats{}, err
		}
		s.Series++
		s.Chunks += len(chks)
		for _, chk := range chks {
			n := index.IngestedAtFieldSize(chk)
			s.IngestedAtBytes += n
			if chk.IngestedAt != 0 {
				s.ChunksWithValue++
			} else {
				s.SentinelBytes += n
			}
		}
	}
	if err := postings.Err(); err != nil {
		return stats{}, err
	}
	return s, nil
}

// maybeGunzip decompresses gzipped index files to a temp file so the mmap
// based reader can open them.
func maybeGunzip(path string) (string, func(), error) {
	if !strings.HasSuffix(path, ".gz") {
		return path, func() {}, nil
	}

	src, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return "", nil, err
	}
	defer gz.Close()

	dst, err := os.CreateTemp("", strings.TrimSuffix(filepath.Base(path), ".gz")+"-*")
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(dst, gz); err != nil {
		dst.Close()
		os.Remove(dst.Name())
		return "", nil, err
	}
	if err := dst.Close(); err != nil {
		os.Remove(dst.Name())
		return "", nil, err
	}
	return dst.Name(), func() { os.Remove(dst.Name()) }, nil
}
