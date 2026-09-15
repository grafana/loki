package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

var encodingNames = map[string]format.PostingsEncoding{
	"uint32":      format.PostingsEncodingFastUint32Blocked,
	"roaring":     format.PostingsEncodingFastRoaringBlocked,
	"deltavarint": format.PostingsEncodingFastDeltaVarIntBlocked,
}

func validEncodings() string {
	names := make([]string, 0, len(encodingNames))
	for k := range encodingNames {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func runConvert(args []string) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	threshold := fs.Float64("threshold", 0.20, "density threshold — filter terms in more than this fraction of docs (0 to disable)")
	encoding := fs.String("encoding", "deltavarint", "output bitmap encoding: "+validEncodings())
	outVersion := fs.String("version", logline.CurrentVersion, "output format version (currently: v3)")
	doVerify := fs.Bool("verify", false, "after conversion, verify every term matches between input and output")
	reportOut := fs.String("report-out", "", "generate an HTML report at this path (empty = no report)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: logline-util convert [flags] <src-index> [dst-index]\n\n")
		fmt.Fprintf(os.Stderr, "Convert a logline index to a new format.\n")
		fmt.Fprintf(os.Stderr, "If dst-index is omitted, writes to <src-index>.converted\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	enc, ok := encodingNames[*encoding]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown encoding: %s\nValid: %s\n", *encoding, validEncodings())
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	src := fs.Arg(0)
	dst := src + ".converted"
	if fs.NArg() >= 2 {
		dst = fs.Arg(1)
	}

	stats, err := convertIndex(src, dst, *outVersion, float32(*threshold), enc, *encoding)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *doVerify {
		vStats, err := verifyIndexes(src, dst)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Verify error: %v\n", err)
			os.Exit(1)
		}
		stats.Verify = vStats
	}

	if *reportOut != "" {
		fmt.Fprintf(os.Stderr, "=== READ BENCHMARK ===\n")
		inputBench, err := benchmarkReads(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Input benchmark error: %v\n", err)
			os.Exit(1)
		}
		outputBench, err := benchmarkReads(dst)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Output benchmark error: %v\n", err)
			os.Exit(1)
		}
		stats.InputBench = inputBench
		stats.OutputBench = outputBench

		if err := writeReport(*reportOut, stats); err != nil {
			fmt.Fprintf(os.Stderr, "Report error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Report: %s\n", *reportOut)
	}
}

type convertStats struct {
	SrcPath      string
	DstPath      string
	Terms        int
	Docs         uint32
	Threshold    float32
	EncodingName string

	SrcBytes int64
	DstBytes int64
	Duration time.Duration

	Verify      *verifyStats
	InputBench  *benchStats
	OutputBench *benchStats
}

func convertIndex(src, dst, version string, threshold float32, enc format.PostingsEncoding, encName string) (*convertStats, error) {
	reader, srcVersion, err := logline.OpenFile(src)
	if err != nil {
		return nil, fmt.Errorf("open input index: %w", err)
	}
	defer reader.Close()

	header := reader.ReadHeader()
	fmt.Fprintf(os.Stderr, "=== CONVERT ===\n")
	fmt.Fprintf(os.Stderr, "Input:     %s (%s)\n", src, srcVersion)
	fmt.Fprintf(os.Stderr, "Output:    %s (%s)\n", dst, version)
	fmt.Fprintf(os.Stderr, "Terms:     %d\n", header.TermCount)
	fmt.Fprintf(os.Stderr, "Docs:      %d\n", header.DocumentCount)
	fmt.Fprintf(os.Stderr, "Encoding:  %s\n", encName)
	fmt.Fprintf(os.Stderr, "Threshold: %.2f\n", threshold)
	fmt.Fprintf(os.Stderr, "\n")

	cfg := format.WriterConfig{
		Encoding:         enc,
		DensityThreshold: threshold,
	}

	writer, err := logline.NewWriter(version, dst, reader.Documents(), &cfg)
	if err != nil {
		return nil, fmt.Errorf("create output writer: %w", err)
	}

	it, err := reader.NewTermIterator()
	if err != nil {
		return nil, fmt.Errorf("create term iterator: %w", err)
	}

	start := time.Now()
	count := 0
	for it.Next() {
		if err := writer.WriteTermBitmap(it.Term(), it.Bitmap()); err != nil {
			return nil, fmt.Errorf("write term %d: %w", count, err)
		}
		count++
		if count%5_000_000 == 0 {
			fmt.Fprintf(os.Stderr, "  converted %d terms...\n", count)
		}
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate input terms: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close output writer: %w", err)
	}

	elapsed := time.Since(start)
	srcFi, _ := os.Stat(src)
	dstFi, _ := os.Stat(dst)
	srcMB := float64(srcFi.Size()) / 1024 / 1024
	dstMB := float64(dstFi.Size()) / 1024 / 1024
	pct := (dstMB - srcMB) / srcMB * 100

	fmt.Fprintf(os.Stderr, "Converted %d terms in %s.\n", count, elapsed.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "Input size:  %.1f MB\n", srcMB)
	fmt.Fprintf(os.Stderr, "Output size: %.1f MB\n", dstMB)
	fmt.Fprintf(os.Stderr, "Delta:       %+.1f%% (%+.1f MB)\n\n", pct, dstMB-srcMB)

	return &convertStats{
		SrcPath: src, DstPath: dst, Terms: count, Docs: header.DocumentCount,
		Threshold: threshold, EncodingName: encName,
		SrcBytes: srcFi.Size(), DstBytes: dstFi.Size(), Duration: elapsed,
	}, nil
}
