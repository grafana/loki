package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"time"
)

type verifyStats struct {
	Total      int
	Matched    int
	Sentinel   int
	Mismatch   int
	OutMissing int
	OutExtra   int
	DocsA      uint32
	DocsB      uint32
	Duration   time.Duration
}

type benchStats struct {
	Label        string
	Queries      int
	TotalIOs     int64
	TotalIOBytes int64
	Duration     time.Duration
}

func runCompare(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	reportOut := fs.String("report-out", "", "generate an HTML report at this path (empty = no report)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: logline-util compare [flags] <index-a> <index-b>\n\n")
		fmt.Fprintf(os.Stderr, "Compare two logline indexes: verify term-by-term equality and optionally\n")
		fmt.Fprintf(os.Stderr, "benchmark read performance. Indexes can be any supported version.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(1)
	}

	pathA := fs.Arg(0)
	pathB := fs.Arg(1)

	vStats, err := verifyIndexes(pathA, pathB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Verify error: %v\n", err)
		os.Exit(1)
	}

	if *reportOut != "" {
		fmt.Fprintf(os.Stderr, "=== READ BENCHMARK ===\n")
		benchA, err := benchmarkReads(pathA)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Benchmark error (%s): %v\n", pathA, err)
			os.Exit(1)
		}
		benchB, err := benchmarkReads(pathB)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Benchmark error (%s): %v\n", pathB, err)
			os.Exit(1)
		}

		srcFi, _ := os.Stat(pathA)
		dstFi, _ := os.Stat(pathB)

		stats := &convertStats{
			SrcPath:  pathA,
			DstPath:  pathB,
			Docs:     vStats.DocsA,
			SrcBytes: srcFi.Size(),
			DstBytes: dstFi.Size(),

			Verify:      vStats,
			InputBench:  benchA,
			OutputBench: benchB,
		}

		if err := writeReport(*reportOut, stats); err != nil {
			fmt.Fprintf(os.Stderr, "Report error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Report: %s\n", *reportOut)
	}
}

// verifyIndexes compares two indexes term-by-term.
// Both indexes are auto-detected.
func verifyIndexes(pathA, pathB string) (*verifyStats, error) {
	fmt.Fprintf(os.Stderr, "=== VERIFY ===\n")
	start := time.Now()

	handleA, err := openIndex(pathA)
	if err != nil {
		return nil, fmt.Errorf("open index A: %w", err)
	}
	defer handleA.Close()

	handleB, err := openIndex(pathB)
	if err != nil {
		return nil, fmt.Errorf("open index B: %w", err)
	}
	defer handleB.Close()

	fmt.Fprintf(os.Stderr, "Index A: %s (%s, %d docs)\n", pathA, handleA.version(), handleA.documentCount())
	fmt.Fprintf(os.Stderr, "Index B: %s (%s, %d docs)\n", pathB, handleB.version(), handleB.documentCount())

	itA, err := handleA.newTermIterator()
	if err != nil {
		return nil, fmt.Errorf("term iterator A: %w", err)
	}
	itB, err := handleB.newTermIterator()
	if err != nil {
		return nil, fmt.Errorf("term iterator B: %w", err)
	}

	s := &verifyStats{DocsA: handleA.documentCount(), DocsB: handleB.documentCount()}
	for itA.Next() {
		s.Total++
		inTerm := itA.Term()
		inBM := itA.Bitmap()

		if !itB.Next() {
			s.OutMissing++
			if s.OutMissing <= 10 {
				fmt.Fprintf(os.Stderr, "  MISSING in B: term %q (A has %d docs)\n",
					formatTerm(inTerm), inBM.Roaring.GetCardinality())
			}
			continue
		}
		outTerm := itB.Term()
		outBM := itB.Bitmap()

		if inTerm != outTerm {
			return nil, fmt.Errorf("term order mismatch at position %d: A=%q B=%q",
				s.Total, formatTerm(inTerm), formatTerm(outTerm))
		}

		if outBM.MatchesAll {
			s.Sentinel++
			if s.Sentinel <= 20 {
				fmt.Fprintf(os.Stderr, "  SENTINEL: %q — A had %d docs (%.1f%% of %d)\n",
					formatTerm(inTerm), inBM.Roaring.GetCardinality(),
					float64(inBM.Roaring.GetCardinality())/float64(handleA.documentCount())*100,
					handleA.documentCount())
			}
			continue
		}

		if inBM.Roaring.Equals(outBM.Roaring) {
			s.Matched++
		} else {
			s.Mismatch++
			if s.Mismatch <= 10 {
				inOnly := inBM.Roaring.Clone()
				inOnly.AndNot(outBM.Roaring)
				outOnly := outBM.Roaring.Clone()
				outOnly.AndNot(inBM.Roaring)
				fmt.Fprintf(os.Stderr, "  MISMATCH: %q — A=%d, B=%d (A-only: %d, B-only: %d)\n",
					formatTerm(inTerm), inBM.Roaring.GetCardinality(), outBM.Roaring.GetCardinality(),
					inOnly.GetCardinality(), outOnly.GetCardinality())
			}
		}

		if s.Total%5_000_000 == 0 {
			fmt.Fprintf(os.Stderr, "  verified %d terms...\n", s.Total)
		}
	}
	if err := itA.Err(); err != nil {
		return nil, fmt.Errorf("iteration error (A): %w", err)
	}
	if err := itB.Err(); err != nil {
		return nil, fmt.Errorf("iteration error (B): %w", err)
	}
	for itB.Next() {
		s.OutExtra++
	}

	s.Duration = time.Since(start)

	fmt.Fprintf(os.Stderr, "\n=== RESULTS ===\n")
	fmt.Fprintf(os.Stderr, "Total terms:      %d\n", s.Total)
	fmt.Fprintf(os.Stderr, "Exact match:      %d\n", s.Matched)
	fmt.Fprintf(os.Stderr, "Sentinel (dense): %d\n", s.Sentinel)
	fmt.Fprintf(os.Stderr, "Mismatched:       %d\n", s.Mismatch)
	fmt.Fprintf(os.Stderr, "Missing in B:     %d\n", s.OutMissing)
	fmt.Fprintf(os.Stderr, "Extra in B:       %d\n", s.OutExtra)
	if s.Sentinel > 20 {
		fmt.Fprintf(os.Stderr, "  (showing first 20 of %d sentinel terms)\n", s.Sentinel)
	}
	if s.Mismatch > 0 || s.OutMissing > 0 || s.OutExtra > 0 {
		fmt.Fprintf(os.Stderr, "\nWARNING: unexpected diffs found (sentinels are expected)\n")
	} else {
		fmt.Fprintf(os.Stderr, "\nOK: all non-sentinel terms match exactly.\n")
	}
	fmt.Fprintf(os.Stderr, "Verify took %s\n\n", s.Duration.Round(time.Millisecond))
	return s, nil
}

// benchmarkReads measures random single-term query I/O on an index file.
func benchmarkReads(path string) (*benchStats, error) {
	const numQueries = 200

	handle, err := openIndex(path)
	if err != nil {
		return nil, err
	}

	terms, err := handle.collectTerms()
	handle.Close()
	if err != nil {
		return nil, err
	}
	sampleTerms := sampleN(terms, numQueries)

	counter, queryFn, closer, err := openForBenchmark(handle)
	if err != nil {
		return nil, err
	}
	defer closer()

	counter.reset()
	runtime.GC()
	start := time.Now()

	for _, term := range sampleTerms {
		_ = queryFn([]string{term})
	}

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "  %s (%s): %d queries, %d I/Os, %.1f MB read, %s\n",
		path, handle.version(), len(sampleTerms), counter.count.Load(), float64(counter.bytes.Load())/1024/1024,
		elapsed.Round(time.Millisecond))

	return &benchStats{
		Label: path, Queries: len(sampleTerms),
		TotalIOs: counter.count.Load(), TotalIOBytes: counter.bytes.Load(), Duration: elapsed,
	}, nil
}

func sampleN(all []string, n int) []string {
	if len(all) <= n {
		return all
	}
	rng := rand.New(rand.NewSource(42))
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	return all[:n]
}
