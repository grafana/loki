package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

func main() {
	var (
		query    string
		refsFile string
		verbose  bool
	)

	flag.StringVar(&query, "query", "", "LogQL query to test (e.g. '{app=\"foo\"} | key=\"value\"')")
	flag.StringVar(&refsFile, "refs", "", "File of chunk refs to test, one per line: <fp_hex> <from_ms> <through_ms> <checksum_hex>")
	flag.BoolVar(&verbose, "v", false, "Print per-series and per-chunk results")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: go run main.go -query '<logql>' -refs <file> [-v] BLOCK_DIRECTORY")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Refs file format (one chunk ref per line, # comments allowed):")
		fmt.Fprintln(os.Stderr, "  <fingerprint_hex> <from_unix_ms> <through_unix_ms> <checksum_hex>")
		fmt.Fprintln(os.Stderr, "Example:")
		fmt.Fprintln(os.Stderr, "  1a2b3c4d5e6f7890 1700000000000 1700000060000 deadbeef")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 || query == "" || refsFile == "" {
		flag.Usage()
		os.Exit(2)
	}

	blockDir := flag.Arg(0)

	// Parse query and extract testable label matchers.
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse query %q: %v\n", query, err)
		os.Exit(1)
	}
	matchers := v1.ExtractTestableLabelMatchers(expr)
	bloomTest := v1.LabelMatchersToBloomTest(matchers...)

	// Parse refs file, grouped by fingerprint.
	grouped, err := parseRefsFile(refsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse refs file: %v\n", err)
		os.Exit(1)
	}

	// Sort fingerprints ascending — the block is sorted the same way, so we
	// can walk both in order with a single Seek per fingerprint.
	fps := make([]model.Fingerprint, 0, len(grouped))
	for fp := range grouped {
		fps = append(fps, fp)
	}
	sort.Slice(fps, func(i, j int) bool { return fps[i] < fps[j] })

	fmt.Printf("Block:    %s\n", blockDir)
	fmt.Printf("Query:    %s\n", query)
	fmt.Printf("Matchers: %d extracted\n", len(matchers))
	for i, m := range matchers {
		fmt.Printf("  [%d] %T %+v\n", i, m, m)
	}
	fmt.Printf("Input:    %d unique fingerprints, %d total chunk refs\n",
		len(fps), totalRefs(grouped))
	fmt.Println()

	// Open block.
	r := v1.NewDirectoryBlockReader(blockDir)
	b := v1.NewBlock(r, v1.NewMetrics(nil))
	q := v1.NewBlockQuerier(b, &mempool.SimpleHeapAllocator{}, 256<<20)

	md, err := q.Metadata()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load block metadata: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Checksum: 0x%x\n", md.Checksum)
	fmt.Printf("Series:   %+v\n", md.Series)
	fmt.Println()

	// Use BlockQuerierIter to get SeriesWithBlooms (bloom access is via the iter).
	qIter := q.Iter()

	// Series labels are not stored in the bloom block (only the fingerprint).
	// Pass some (random) labels so matching relies solely on the bloom filter.
	// Empty labels will result in short circuting in the matcher since it is not
	// able to disambiguate labels from structured metadata.
	seriesLabels := labels.FromMap(map[string]string{
		"cloud_availability_zone":     "us-east-1e",
		"cloud_region":                "us-east-1",
		"deployment_environment_name": "production",
		"service_name":                "main",
		"severity_text":               "error",
	})

	var (
		totalChunks    int
		matchedChunks  int
		filteredChunks int
		missedChunks   int // input chunks not found in this block at all
		emptyChunks    int // series has no bloom offsets; chunks pass through
	)

	for _, fp := range fps {
		inputChks := grouped[fp]
		totalChunks += len(inputChks)

		// Seek the series iterator to this fingerprint.
		if err := qIter.Seek(fp); err != nil {
			fmt.Fprintf(os.Stderr, "seek error for fp %x: %v\n", fp, err)
			os.Exit(1)
		}

		if !qIter.Next() {
			if verbose {
				fmt.Printf("fp=%x NOT IN BLOCK (missed %d chunks)\n", fp, len(inputChks))
			}
			missedChunks += len(inputChks)
			continue
		}

		swb := qIter.At()
		if swb.Series.Fingerprint != fp {
			// The next series in the block has a higher fingerprint — fp is absent.
			if verbose {
				fmt.Printf("fp=%x NOT IN BLOCK (missed %d chunks)\n", fp, len(inputChks))
			}
			missedChunks += len(inputChks)
			continue
		}

		// Split input chunks into those present in this series and those absent.
		sort.Sort(v1.ChunkRefs(inputChks))
		missing, inBlooms := v1.ChunkRefs(inputChks).Compare(swb.Series.Chunks, true)
		missedChunks += len(missing)

		if len(swb.Series.Offsets) == 0 {
			// Series has no bloom offsets (no structured metadata). Pass through.
			if verbose {
				fmt.Printf("fp=%x EMPTY BLOOM pass-through=%d missing=%d\n",
					fp, len(inBlooms), len(missing))
			}
			emptyChunks += len(inBlooms)
			matchedChunks += len(inBlooms)
			continue
		}

		// A chunk is kept (matched) if ANY bloom for the series passes the test.
		// A chunk is filtered only when ALL blooms fail.
		// We model this with a per-chunk "found" flag; any passing bloom sets it.
		found := make([]bool, len(inBlooms))

		for swb.Blooms.Next() {
			bloom := swb.Blooms.At()

			if bloom.IsEmpty() {
				// Empty bloom: don't filter any chunk.
				for k := range found {
					found[k] = true
				}
				continue
			}

			if bloomTest.Matches(seriesLabels, bloom) {
				for k := range found {
					found[k] = true
				}
			}
		}

		if err := swb.Blooms.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "bloom iterator error for fp %x: %v\n", fp, err)
			os.Exit(1)
		}

		seriesFiltered := 0
		seriesMatched := 0
		for k, chk := range inBlooms {
			if found[k] {
				seriesMatched++
				matchedChunks++
			} else {
				seriesFiltered++
				filteredChunks++
			}
			if verbose {
				status := "MATCH"
				if !found[k] {
					status = "FILTERED"
				}
				fmt.Printf("  fp=%x from=%d through=%d checksum=%08x [%s]\n",
					fp, chk.From, chk.Through, chk.Checksum, status)
			}
		}

		if verbose {
			fmt.Printf("fp=%x matched=%d filtered=%d missing=%d\n",
				fp, seriesMatched, seriesFiltered, len(missing))
		}
	}

	if q.Err() != nil {
		fmt.Fprintf(os.Stderr, "iterator error: %v\n", q.Err())
		os.Exit(1)
	}

	fmt.Println("--- Results ---")
	fmt.Printf("Total chunks:    %d\n", totalChunks)
	fmt.Printf("Matched:         %d (%.1f%%)\n", matchedChunks, pct(matchedChunks, totalChunks))
	fmt.Printf("Filtered:        %d (%.1f%%)\n", filteredChunks, pct(filteredChunks, totalChunks))
	fmt.Printf("Not in block:    %d (%.1f%%)\n", missedChunks, pct(missedChunks, totalChunks))
	fmt.Printf("Empty bloom:     %d (%.1f%%)\n", emptyChunks, pct(emptyChunks, totalChunks))
}

// parseRefsFile reads the refs file and groups ChunkRefs by fingerprint.
// Format per line: <fp_hex> <from_ms> <through_ms> <checksum_hex>
func parseRefsFile(path string) (map[model.Fingerprint][]v1.ChunkRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[model.Fingerprint][]v1.ChunkRef)
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 {
			return nil, fmt.Errorf("line %d: expected 4 fields (fp_hex from_ms through_ms checksum_hex), got %d", lineNo, len(fields))
		}

		fpVal, err := strconv.ParseUint(fields[0], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid fingerprint %q: %v", lineNo, fields[0], err)
		}
		from, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid from_ms %q: %v", lineNo, fields[1], err)
		}
		through, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid through_ms %q: %v", lineNo, fields[2], err)
		}
		checksum, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid checksum %q: %v", lineNo, fields[3], err)
		}

		fp := model.Fingerprint(fpVal)
		result[fp] = append(result[fp], v1.ChunkRef{
			From:     model.Time(from),
			Through:  model.Time(through),
			Checksum: uint32(checksum),
		})
	}
	return result, scanner.Err()
}

func totalRefs(grouped map[model.Fingerprint][]v1.ChunkRef) int {
	n := 0
	for _, refs := range grouped {
		n += len(refs)
	}
	return n
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}
