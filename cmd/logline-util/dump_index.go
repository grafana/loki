package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

func runDumpIndex(args []string) {
	fs := flag.NewFlagSet("dump-index", flag.ExitOnError)

	showTerms := fs.Bool("terms", false, "Show terms list")
	showDocs := fs.Bool("docs", false, "Show document metadata")
	showBitmaps := fs.Bool("bitmaps", false, "Show bitmap contents (document IDs)")
	limit := fs.Int("limit", 0, "Limit output (0 = unlimited)")
	verbose := fs.Bool("verbose", false, "Show everything (-terms -docs -bitmaps)")
	fs.BoolVar(verbose, "v", false, "Show everything (alias for -verbose)")
	reportOut := fs.String("report-out", "", "generate an HTML report at this path (empty = no report)")
	gramFreq := fs.Bool("gram-frequency", false, "include n-gram frequency distribution in the report (requires -report-out)")
	gramSamples := fs.Int("gram-samples", 2000, "number of sample points for the frequency curve")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: logline-util dump-index [flags] <file.lidx>\n\n")
		fmt.Fprintf(os.Stderr, "Inspect logline index (.lidx) files.\n")
		fmt.Fprintf(os.Stderr, "Output goes to stdout by default. Use -report-out for an HTML report.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "Error: missing file argument\n\n")
		fs.Usage()
		os.Exit(1)
	}

	if *verbose {
		*showTerms = true
		*showDocs = true
		*showBitmaps = true
	}

	filePath := fs.Arg(0)

	reader, version, err := logline.OpenFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open file: %v\n", err)
		os.Exit(1)
	}
	defer reader.Close()

	// Text output to stdout (always).
	printHeader(reader, version, filePath)

	if *showTerms {
		it, err := reader.NewTermIterator()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create term iterator: %v\n", err)
			os.Exit(1)
		}
		printTerms(it, *showBitmaps, *limit)
	}

	if *showDocs {
		printDocuments(reader, *limit)
	}

	// HTML report (optional).
	if *reportOut != "" {
		if err := writeDumpReport(*reportOut, reader, version, filePath, *gramFreq, *gramSamples); err != nil {
			fmt.Fprintf(os.Stderr, "Report error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Report: %s\n", *reportOut)
	}
}

func printHeader(reader logline.Reader, version, filePath string) {
	printHeaderToWriter(os.Stdout, reader, version, filePath)
}

func printHeaderToWriter(w io.Writer, reader logline.Reader, version, filePath string) {
	header := reader.ReadHeader()

	fileSize := uint64(0)
	if fileInfo, err := os.Stat(filePath); err == nil {
		fileSize = uint64(fileInfo.Size())
	}

	fmt.Fprintf(w, "File: %s\n", filePath)
	fmt.Fprintf(w, "Version: %s (on-disk: %d)\n", version, header.Version)
	fmt.Fprintf(w, "Endian: little-endian\n")

	postingCount := uint64(0)
	it, err := reader.NewTermIterator()
	if err == nil {
		for it.Next() {
			bm := it.Bitmap()
			if !bm.MatchesAll {
				postingCount += bm.Roaring.GetCardinality()
			}
		}
	}

	fmt.Fprintf(w, "Term Count: %s\n", formatNumber(header.TermCount))
	fmt.Fprintf(w, "Document Count: %s\n", formatNumber(uint64(header.DocumentCount)))
	fmt.Fprintf(w, "Posting Count: %s\n", formatNumber(postingCount))
	fmt.Fprintf(w, "File Size: %s\n", formatBytes(fileSize))
}

func printTerms(it format.TermIterator, showBitmaps bool, limit int) {
	printTermsToWriter(os.Stdout, it, showBitmaps, limit)
}

func printTermsToWriter(w io.Writer, it format.TermIterator, showBitmaps bool, limit int) {
	type termInfo struct {
		term   [8]byte
		bitmap format.Bitmap
	}

	var terms []termInfo
	for it.Next() {
		bm := it.Bitmap()
		if !bm.MatchesAll && bm.Roaring != nil {
			bm = format.Bitmap{Roaring: bm.Roaring.Clone()}
		}
		terms = append(terms, termInfo{term: it.Term(), bitmap: bm})
	}
	if err := it.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to iterate terms: %v\n", err)
		return
	}

	count := len(terms)
	fmt.Fprintf(w, "\n")

	if limit > 0 && count > limit {
		fmt.Fprintf(w, "Terms (%s total, showing first %s):\n",
			formatNumber(uint64(count)), formatNumber(uint64(limit)))
	} else {
		fmt.Fprintf(w, "Terms (%s total):\n", formatNumber(uint64(count)))
	}

	shown := 0
	for i, term := range terms {
		if limit > 0 && shown >= limit {
			break
		}
		shown++
		termStr := formatTerm(term.term)
		bitmapStr := formatBitmap(term.bitmap.Roaring, showBitmaps)
		fmt.Fprintf(w, "  %s. \"%s\" → %s\n",
			formatNumber(uint64(i+1)), termStr, bitmapStr)
	}

	if limit > 0 && count > limit {
		remaining := count - limit
		fmt.Fprintf(w, "\n... and %s more terms\n", formatNumber(uint64(remaining)))
	}
}

func printDocuments(reader logline.Reader, limit int) {
	printDocumentsToWriter(os.Stdout, reader, limit)
}

func printDocumentsToWriter(w io.Writer, reader logline.Reader, limit int) {
	docs := make([]format.DocumentMetadata, len(reader.Documents()))
	copy(docs, reader.Documents())

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].MinTimeUnix < docs[j].MinTimeUnix
	})

	fmt.Fprintf(w, "\n")

	if limit > 0 && len(docs) > limit {
		fmt.Fprintf(w, "Documents (%s total, showing first %s):\n",
			formatNumber(uint64(len(docs))), formatNumber(uint64(limit)))
	} else {
		fmt.Fprintf(w, "Documents (%s total):\n", formatNumber(uint64(len(docs))))
	}

	shown := 0
	for _, doc := range docs {
		if limit > 0 && shown >= limit {
			break
		}
		shown++
		minTime := formatTime(doc.MinTimeUnix)
		maxTime := formatTime(doc.MaxTimeUnix)
		fmt.Fprintf(w, "  Doc %s: %s - %s\n",
			formatNumber(uint64(doc.ID)), minTime, maxTime)
	}

	if limit > 0 && len(docs) > limit {
		remaining := len(docs) - limit
		fmt.Fprintf(w, "\n... and %s more document", formatNumber(uint64(remaining)))
		if remaining != 1 {
			fmt.Fprintf(w, "s")
		}
		fmt.Fprintf(w, "\n")
	}
}
