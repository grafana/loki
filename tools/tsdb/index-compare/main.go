package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type fileStats struct {
	label            string
	compressedSize   int64
	uncompressedSize int64
	totalSeries      int
	totalChunks      int64
	chunksPerSeries  []int
	uniqueChecksums  map[uint32]struct{}
}

func main() {
	dataobjPath := flag.String("dataobj", "", "path to the dataobj compacted .tsdb.gz file")
	chunksPath := flag.String("chunks", "", "path to the chunk-based compacted .tsdb.gz file")
	flag.Parse()

	if *dataobjPath == "" || *chunksPath == "" {
		fmt.Fprintln(os.Stderr, "usage: index-compare --dataobj <path.tsdb.gz> --chunks <path.tsdb.gz>")
		os.Exit(1)
	}

	fmt.Println("=== Analyzing chunk-based TSDB ===")
	chunkStats, err := analyzeFile(*chunksPath, "chunks")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error analyzing chunks file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n=== Analyzing dataobj TSDB ===")
	dataobjStats, err := analyzeFile(*dataobjPath, "dataobj")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error analyzing dataobj file: %v\n", err)
		os.Exit(1)
	}

	printComparison(chunkStats, dataobjStats)
}

func analyzeFile(gzPath, label string) (*fileStats, error) {
	info, err := os.Stat(gzPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", gzPath, err)
	}

	stats := &fileStats{
		label:           label,
		compressedSize:  info.Size(),
		uniqueChecksums: make(map[uint32]struct{}),
	}

	fmt.Printf("compressed size: %s\n", humanBytes(stats.compressedSize))
	fmt.Println("decompressing to temp file...")

	tmpFile, err := decompressToTemp(gzPath)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer func() {
		name := tmpFile.Name()
		tmpFile.Close()
		fmt.Printf("cleaning up temp file: %s\n", name)
		os.Remove(name)
	}()

	tmpInfo, err := tmpFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat temp: %w", err)
	}
	stats.uncompressedSize = tmpInfo.Size()
	fmt.Printf("uncompressed size: %s\n", humanBytes(stats.uncompressedSize))

	tmpFile.Close()

	fmt.Println("opening TSDB index...")
	reader, err := index.NewFileReader(tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer reader.Close()

	fmt.Println("iterating series...")
	p, err := reader.Postings("", nil, "")
	if err != nil {
		return nil, fmt.Errorf("get postings: %w", err)
	}

	var (
		ls   labels.Labels
		chks []index.ChunkMeta
	)

	seriesCount := 0
	for p.Next() {
		chks = chks[:0]
		_, err := reader.Series(p.At(), 0, math.MaxInt64, &ls, &chks)
		if err != nil {
			return nil, fmt.Errorf("read series: %w", err)
		}

		nChunks := len(chks)
		stats.totalSeries++
		stats.totalChunks += int64(nChunks)
		stats.chunksPerSeries = append(stats.chunksPerSeries, nChunks)

		for i := range chks {
			stats.uniqueChecksums[chks[i].Checksum] = struct{}{}
		}

		seriesCount++
		if seriesCount%100000 == 0 {
			fmt.Printf("  processed %d series (%d chunks so far)...\n", seriesCount, stats.totalChunks)
		}
	}
	if err := p.Err(); err != nil {
		return nil, fmt.Errorf("postings iteration: %w", err)
	}

	fmt.Printf("done: %d series, %d chunk metas\n", stats.totalSeries, stats.totalChunks)
	return stats, nil
}

func decompressToTemp(gzPath string) (*os.File, error) {
	src, err := os.Open(gzPath)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tmp, err := os.CreateTemp("", "tsdb-compare-*.tsdb")
	if err != nil {
		return nil, err
	}

	n, err := io.Copy(tmp, gz)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("decompress copy (%d bytes written): %w", n, err)
	}

	fmt.Printf("decompressed %s to %s\n", humanBytes(n), tmp.Name())
	return tmp, nil
}

func printComparison(chunkStats, dataobjStats *fileStats) {
	sort.Ints(chunkStats.chunksPerSeries)
	sort.Ints(dataobjStats.chunksPerSeries)

	fmt.Println("\n========================================")
	fmt.Println("         TSDB COMPARISON RESULTS")
	fmt.Println("========================================")

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.AlignRight)

	fmt.Fprintf(w, "\t%s\t%s\t\n", "chunks", "dataobj")
	fmt.Fprintf(w, "compressed size\t%s\t%s\t\n", humanBytes(chunkStats.compressedSize), humanBytes(dataobjStats.compressedSize))
	fmt.Fprintf(w, "uncompressed size\t%s\t%s\t\n", humanBytes(chunkStats.uncompressedSize), humanBytes(dataobjStats.uncompressedSize))
	fmt.Fprintf(w, "total series\t%d\t%d\t\n", chunkStats.totalSeries, dataobjStats.totalSeries)
	fmt.Fprintf(w, "total chunk metas\t%d\t%d\t\n", chunkStats.totalChunks, dataobjStats.totalChunks)

	if chunkStats.totalSeries > 0 && dataobjStats.totalSeries > 0 {
		chunkAvg := float64(chunkStats.totalChunks) / float64(chunkStats.totalSeries)
		dataobjAvg := float64(dataobjStats.totalChunks) / float64(dataobjStats.totalSeries)
		fmt.Fprintf(w, "avg chunks/series\t%.1f\t%.1f\t\n", chunkAvg, dataobjAvg)
	}

	fmt.Fprintf(w, "median chunks/series\t%d\t%d\t\n",
		percentile(chunkStats.chunksPerSeries, 50),
		percentile(dataobjStats.chunksPerSeries, 50))
	fmt.Fprintf(w, "p95 chunks/series\t%d\t%d\t\n",
		percentile(chunkStats.chunksPerSeries, 95),
		percentile(dataobjStats.chunksPerSeries, 95))
	fmt.Fprintf(w, "p99 chunks/series\t%d\t%d\t\n",
		percentile(chunkStats.chunksPerSeries, 99),
		percentile(dataobjStats.chunksPerSeries, 99))
	fmt.Fprintf(w, "max chunks/series\t%d\t%d\t\n",
		maxVal(chunkStats.chunksPerSeries),
		maxVal(dataobjStats.chunksPerSeries))

	fmt.Fprintf(w, "series > 100 chunks\t%d\t%d\t\n",
		countOver(chunkStats.chunksPerSeries, 100),
		countOver(dataobjStats.chunksPerSeries, 100))
	fmt.Fprintf(w, "series > 1k chunks\t%d\t%d\t\n",
		countOver(chunkStats.chunksPerSeries, 1000),
		countOver(dataobjStats.chunksPerSeries, 1000))
	fmt.Fprintf(w, "series > 10k chunks\t%d\t%d\t\n",
		countOver(chunkStats.chunksPerSeries, 10000),
		countOver(dataobjStats.chunksPerSeries, 10000))

	fmt.Fprintf(w, "unique checksums\t%d\t%d\t\n",
		len(chunkStats.uniqueChecksums), len(dataobjStats.uniqueChecksums))

	estChunkBytes := chunkStats.totalChunks * 14
	estDataobjBytes := dataobjStats.totalChunks * 14
	fmt.Fprintf(w, "est. chunk data bytes\t%s\t%s\t\n",
		humanBytes(estChunkBytes), humanBytes(estDataobjBytes))

	w.Flush()

	if chunkStats.totalChunks > 0 {
		fmt.Printf("\n--- Amplification factor (dataobj / chunks) ---\n")
		fmt.Printf("  compressed size:   %.2fx\n", float64(dataobjStats.compressedSize)/float64(chunkStats.compressedSize))
		fmt.Printf("  uncompressed size: %.2fx\n", float64(dataobjStats.uncompressedSize)/float64(chunkStats.uncompressedSize))
		fmt.Printf("  chunk metas:       %.2fx\n", float64(dataobjStats.totalChunks)/float64(chunkStats.totalChunks))
		if chunkStats.totalSeries > 0 && dataobjStats.totalSeries > 0 {
			chunkAvg := float64(chunkStats.totalChunks) / float64(chunkStats.totalSeries)
			dataobjAvg := float64(dataobjStats.totalChunks) / float64(dataobjStats.totalSeries)
			fmt.Printf("  avg chunks/series: %.2fx\n", dataobjAvg/chunkAvg)
		}
	}
}

func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := len(sorted) * p / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func maxVal(sorted []int) int {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[len(sorted)-1]
}

func countOver(sorted []int, threshold int) int {
	idx := sort.SearchInts(sorted, threshold+1)
	return len(sorted) - idx
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
