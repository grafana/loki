package main

import (
	"compress/gzip"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type fileStats struct {
	label            string
	compressedSize   int64
	uncompressedSize int64

	totalSeries        int
	totalChunks        int64
	maxChunksPerSeries int

	sidecarCompressedSize   int64
	sidecarUncompressedSize int64
	sidecarEntries          uint32
	sidecarUniquePaths      uint32
	sidecarPresent          bool
}

func main() {
	dataobjPath := flag.String("dataobj", "", "path to the dataobj compacted .tsdb.gz file")
	chunksPath := flag.String("chunks", "", "path to the chunk-based compacted .tsdb.gz file")
	flag.Parse()

	if *dataobjPath == "" && *chunksPath == "" {
		fmt.Fprintln(os.Stderr, "usage: index-compare [--dataobj <path.tsdb.gz>] [--chunks <path.tsdb.gz>]")
		os.Exit(1)
	}

	var allStats []*fileStats

	if *chunksPath != "" {
		fmt.Fprintf(os.Stderr, "=== Analyzing chunks TSDB ===\n")
		s, err := analyzeFile(*chunksPath, "chunks")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error analyzing chunks file: %v\n", err)
			os.Exit(1)
		}
		allStats = append(allStats, s)
	}

	if *dataobjPath != "" {
		fmt.Fprintf(os.Stderr, "\n=== Analyzing dataobj TSDB ===\n")
		s, err := analyzeFile(*dataobjPath, "dataobj")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error analyzing dataobj file: %v\n", err)
			os.Exit(1)
		}
		allStats = append(allStats, s)
	}

	printResults(allStats)
}

func analyzeFile(gzPath, label string) (*fileStats, error) {
	info, err := os.Stat(gzPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", gzPath, err)
	}

	stats := &fileStats{
		label:          label,
		compressedSize: info.Size(),
	}

	fmt.Fprintf(os.Stderr, "  compressed: %s\n", humanBytes(stats.compressedSize))

	// Analyze sidecar if present.
	sidecarPath := deriveSidecarPath(gzPath)
	if sidecarPath != "" {
		analyzeSidecar(stats, sidecarPath)
	}

	// Decompress TSDB.
	fmt.Fprintf(os.Stderr, "  decompressing TSDB...\n")
	tmpFile, err := decompressToTemp(gzPath)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer func() {
		name := tmpFile.Name()
		tmpFile.Close()
		os.Remove(name)
	}()

	tmpInfo, err := tmpFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat temp: %w", err)
	}
	stats.uncompressedSize = tmpInfo.Size()
	fmt.Fprintf(os.Stderr, "  uncompressed: %s\n", humanBytes(stats.uncompressedSize))

	tmpFile.Close()

	// Open and iterate.
	fmt.Fprintf(os.Stderr, "  iterating series...\n")
	reader, err := index.NewFileReader(tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer reader.Close()

	p, err := reader.Postings("", nil, "")
	if err != nil {
		return nil, fmt.Errorf("get postings: %w", err)
	}

	var (
		ls   labels.Labels
		chks []index.ChunkMeta
	)

	for p.Next() {
		chks = chks[:0]
		_, err := reader.Series(p.At(), 0, math.MaxInt64, &ls, &chks)
		if err != nil {
			return nil, fmt.Errorf("read series: %w", err)
		}

		n := len(chks)
		stats.totalSeries++
		stats.totalChunks += int64(n)
		if n > stats.maxChunksPerSeries {
			stats.maxChunksPerSeries = n
		}

		if stats.totalSeries%500000 == 0 {
			fmt.Fprintf(os.Stderr, "    %d series, %d chunks...\n", stats.totalSeries, stats.totalChunks)
		}
	}
	if err := p.Err(); err != nil {
		return nil, fmt.Errorf("postings iteration: %w", err)
	}

	fmt.Fprintf(os.Stderr, "  done: %d series, %d chunks\n", stats.totalSeries, stats.totalChunks)
	return stats, nil
}

// deriveSidecarPath converts a .tsdb.gz path to the corresponding .tsdb.sections.gz path.
func deriveSidecarPath(tsdbGzPath string) string {
	if strings.HasSuffix(tsdbGzPath, ".tsdb.gz") {
		return strings.TrimSuffix(tsdbGzPath, ".gz") + ".sections.gz"
	}
	return ""
}

// analyzeSidecar parses the sidecar .sections.gz file header to extract
// path count and entry count without loading the full table into memory.
func analyzeSidecar(stats *fileStats, path string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  sidecar: not found (%s)\n", path)
		return
	}

	stats.sidecarPresent = true
	stats.sidecarCompressedSize = info.Size()
	fmt.Fprintf(os.Stderr, "  sidecar compressed: %s\n", humanBytes(stats.sidecarCompressedSize))

	fmt.Fprintf(os.Stderr, "  decompressing sidecar...\n")
	tmpFile, err := decompressToTemp(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  sidecar decompress failed: %v\n", err)
		return
	}
	defer func() {
		name := tmpFile.Name()
		tmpFile.Close()
		os.Remove(name)
	}()

	tmpInfo, err := tmpFile.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  sidecar stat failed: %v\n", err)
		return
	}
	stats.sidecarUncompressedSize = tmpInfo.Size()
	fmt.Fprintf(os.Stderr, "  sidecar uncompressed: %s\n", humanBytes(stats.sidecarUncompressedSize))

	// Parse just the header to get pathCount and entryCount without loading everything.
	pathCount, entryCount, err := parseSidecarHeader(tmpFile.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "  sidecar header parse failed: %v\n", err)
		return
	}

	stats.sidecarUniquePaths = pathCount
	stats.sidecarEntries = entryCount
	fmt.Fprintf(os.Stderr, "  sidecar entries: %d, unique paths: %d\n", entryCount, pathCount)
}

// parseSidecarHeader reads only the pathCount and entryCount from the binary
// format without loading the full entry table. The format is:
//
//	[uint32 pathCount] [paths...] [uint32 entryCount] [entries...]
func parseSidecarHeader(filePath string) (pathCount, entryCount uint32, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	// Read pathCount.
	if err := binary.Read(f, binary.LittleEndian, &pathCount); err != nil {
		return 0, 0, fmt.Errorf("reading path count: %w", err)
	}

	// Skip over all path strings: each is [uint16 len][string bytes].
	for i := uint32(0); i < pathCount; i++ {
		var slen uint16
		if err := binary.Read(f, binary.LittleEndian, &slen); err != nil {
			return 0, 0, fmt.Errorf("reading path %d length: %w", i, err)
		}
		if _, err := f.Seek(int64(slen), io.SeekCurrent); err != nil {
			return 0, 0, fmt.Errorf("skipping path %d: %w", i, err)
		}
	}

	// Read entryCount.
	if err := binary.Read(f, binary.LittleEndian, &entryCount); err != nil {
		return 0, 0, fmt.Errorf("reading entry count: %w", err)
	}

	return pathCount, entryCount, nil
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

	tmp, err := os.CreateTemp("", "tsdb-compare-*")
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(tmp, gz); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("decompress: %w", err)
	}

	return tmp, nil
}

func printResults(allStats []*fileStats) {
	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.AlignRight)

	// Header.
	fmt.Fprintf(w, "\t")
	for _, s := range allStats {
		fmt.Fprintf(w, "%s\t", s.label)
	}
	fmt.Fprintln(w)

	// TSDB sizes.
	printRow(w, "compressed size", allStats, func(s *fileStats) string { return humanBytes(s.compressedSize) })
	printRow(w, "uncompressed size", allStats, func(s *fileStats) string { return humanBytes(s.uncompressedSize) })

	// Sidecar.
	printRow(w, "sidecar compressed", allStats, func(s *fileStats) string {
		if !s.sidecarPresent {
			return "N/A"
		}
		return humanBytes(s.sidecarCompressedSize)
	})
	printRow(w, "sidecar uncompressed", allStats, func(s *fileStats) string {
		if !s.sidecarPresent {
			return "N/A"
		}
		return humanBytes(s.sidecarUncompressedSize)
	})
	printRow(w, "sidecar entries", allStats, func(s *fileStats) string {
		if !s.sidecarPresent {
			return "N/A"
		}
		return fmt.Sprintf("%d", s.sidecarEntries)
	})
	printRow(w, "sidecar unique paths", allStats, func(s *fileStats) string {
		if !s.sidecarPresent {
			return "N/A"
		}
		return fmt.Sprintf("%d", s.sidecarUniquePaths)
	})

	// Series / chunks.
	printRow(w, "total series", allStats, func(s *fileStats) string { return fmt.Sprintf("%d", s.totalSeries) })
	printRow(w, "total chunk metas", allStats, func(s *fileStats) string { return fmt.Sprintf("%d", s.totalChunks) })
	printRow(w, "avg chunks/series", allStats, func(s *fileStats) string {
		if s.totalSeries == 0 {
			return "0"
		}
		return fmt.Sprintf("%.1f", float64(s.totalChunks)/float64(s.totalSeries))
	})
	printRow(w, "max chunks/series", allStats, func(s *fileStats) string { return fmt.Sprintf("%d", s.maxChunksPerSeries) })

	w.Flush()

	// Amplification factors if we have both.
	if len(allStats) == 2 {
		a, b := allStats[0], allStats[1]
		fmt.Printf("\n--- Amplification (%s / %s) ---\n", b.label, a.label)
		if a.compressedSize > 0 {
			fmt.Printf("  compressed size:   %.2fx\n", float64(b.compressedSize)/float64(a.compressedSize))
		}
		if a.uncompressedSize > 0 {
			fmt.Printf("  uncompressed size: %.2fx\n", float64(b.uncompressedSize)/float64(a.uncompressedSize))
		}
		if a.totalChunks > 0 {
			fmt.Printf("  chunk metas:       %.2fx\n", float64(b.totalChunks)/float64(a.totalChunks))
		}
		if a.totalSeries > 0 && b.totalSeries > 0 {
			avgA := float64(a.totalChunks) / float64(a.totalSeries)
			avgB := float64(b.totalChunks) / float64(b.totalSeries)
			fmt.Printf("  avg chunks/series: %.2fx\n", avgB/avgA)
		}
	}
}

func printRow(w *tabwriter.Writer, label string, allStats []*fileStats, fn func(*fileStats) string) {
	fmt.Fprintf(w, "%s\t", label)
	for _, s := range allStats {
		fmt.Fprintf(w, "%s\t", fn(s))
	}
	fmt.Fprintln(w)
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
