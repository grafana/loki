package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/sectionref"
)

const debugLogPath = "/Users/grafana/Workspace/loki/.cursor/debug-f12cb0.log"

func debugLog(location, message string, data map[string]interface{}, hypothesisID string) {
	entry := map[string]interface{}{
		"sessionId":    "f12cb0",
		"location":     location,
		"message":      message,
		"data":         data,
		"hypothesisId": hypothesisID,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, _ := json.Marshal(entry)
	if f, err := os.OpenFile(debugLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.Write(append(b, '\n'))
		f.Close()
	}
}

func main() {
	tsdbPath := flag.String("tsdb", "", "path to compacted .tsdb.gz file")
	sectionsPath := flag.String("sections", "", "path to companion .tsdb.sections.gz file")
	tenant := flag.String("tenant", "", "tenant ID to query")
	matchersStr := flag.String("matchers", "{}", "LogQL matchers, e.g. {app=\"gateway\"}")
	flag.Parse()

	if *tsdbPath == "" || *sectionsPath == "" || *tenant == "" {
		fmt.Fprintln(os.Stderr, "usage: dataobj-inspect --tsdb <file.tsdb.gz> --sections <file.tsdb.sections.gz> --tenant <id> [--matchers '{...}']")
		os.Exit(1)
	}

	fmt.Println("=== Decompressing TSDB index ===")
	tsdbTmp, err := decompressToTemp(*tsdbPath, "tsdb-inspect-*.tsdb")
	exitErr("decompress tsdb", err)
	defer cleanupTemp(tsdbTmp)

	fmt.Println("\n=== Decompressing sections sidecar ===")
	secTmp, err := decompressToTemp(*sectionsPath, "tsdb-inspect-*.sections")
	exitErr("decompress sections", err)
	defer cleanupTemp(secTmp)

	fmt.Println("\n=== Opening TSDB index ===")
	idx, _, err := tsdb.NewTSDBIndexFromFile(tsdbTmp.Name())
	exitErr("open tsdb index", err)
	defer idx.Close()

	fmt.Println("=== Opening sections table ===")
	table, err := sectionref.OpenMmap(secTmp.Name())
	exitErr("open sections table", err)
	idx.SetSectionRefTable(table)
	fmt.Printf("sections table entries: %d\n", table.Len())

	userMatchers, err := parseMatchers(*matchersStr)
	exitErr("parse matchers", err)

	// --- Diagnostics: detect compacted vs multitenant ---
	fmt.Println("\n=== Index Diagnostics ===")
	labelNames, lnErr := idx.LabelNames(context.Background(), "", model.Earliest, model.Latest)
	exitErr("LabelNames", lnErr)
	fmt.Printf("label names in index (%d total):\n", len(labelNames))
	hasTenantLabel := false
	for _, ln := range labelNames {
		if ln == tsdb.TenantLabel {
			hasTenantLabel = true
		}
		fmt.Printf("  %s\n", ln)
	}

	if hasTenantLabel {
		tenantVals, tvErr := idx.LabelValues(context.Background(), "", model.Earliest, model.Latest, tsdb.TenantLabel)
		exitErr("LabelValues(tenant)", tvErr)
		fmt.Printf("__loki_tenant__ values (%d): %v\n", len(tenantVals), tenantVals)
	} else {
		fmt.Println("NOTE: __loki_tenant__ label NOT present — this is a compacted single-tenant TSDB")
		fmt.Println("      skipping tenant label matcher for queries")
	}

	// #region agent log
	debugLog("main:diagnostics", "label check", map[string]interface{}{
		"hasTenantLabel": hasTenantLabel,
		"labelCount":     len(labelNames),
	}, "H-A")
	// #endregion

	// Build matchers: only add tenant label if it exists in the index
	var queryMatchers []*labels.Matcher
	if hasTenantLabel {
		queryMatchers = append(queryMatchers, labels.MustNewMatcher(labels.MatchEqual, tsdb.TenantLabel, *tenant))
	}
	if len(userMatchers) > 0 {
		queryMatchers = append(queryMatchers, userMatchers...)
	} else {
		queryMatchers = append(queryMatchers, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	}

	fmt.Printf("\neffective matchers: %v\n", queryMatchers)

	fmt.Println("\n=== Stream Analysis ===")
	analyzeStreams(idx, queryMatchers)

	fmt.Println("\n=== Section Resolution ===")
	resolveSections(idx, *tenant, queryMatchers)
}

func analyzeStreams(idx *tsdb.TSDBIndex, matchers []*labels.Matcher) {
	var (
		streamCount     int
		chunkCount      int
		checksums       = make(map[uint32]struct{})
		chunksPerSeries []int
		sampleLabels    []labels.Labels
	)

	err := idx.ForSeries(
		context.Background(), "", nil,
		model.Earliest, model.Latest,
		func(ls labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
			streamCount++
			chunkCount += len(chks)
			chunksPerSeries = append(chunksPerSeries, len(chks))
			for _, c := range chks {
				checksums[c.Checksum] = struct{}{}
			}
			if len(sampleLabels) < 5 {
				sampleLabels = append(sampleLabels, ls.Copy())
			}
			return false
		},
		matchers...,
	)
	exitErr("ForSeries", err)

	sort.Ints(chunksPerSeries)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "streams matched:\t%d\n", streamCount)
	fmt.Fprintf(w, "total chunk metas:\t%d\n", chunkCount)
	fmt.Fprintf(w, "unique checksums:\t%d\n", len(checksums))
	if streamCount > 0 {
		fmt.Fprintf(w, "avg chunks/stream:\t%.1f\n", float64(chunkCount)/float64(streamCount))
		fmt.Fprintf(w, "median chunks/stream:\t%d\n", percentile(chunksPerSeries, 50))
		fmt.Fprintf(w, "p99 chunks/stream:\t%d\n", percentile(chunksPerSeries, 99))
		fmt.Fprintf(w, "max chunks/stream:\t%d\n", chunksPerSeries[len(chunksPerSeries)-1])
	}
	w.Flush()

	if len(sampleLabels) > 0 {
		fmt.Println("\nsample streams (first 5):")
		for i, ls := range sampleLabels {
			fmt.Printf("  [%d] %s\n", i+1, ls.String())
		}
	}

	// #region agent log
	debugLog("analyzeStreams", "result", map[string]interface{}{
		"streamCount": streamCount, "chunkCount": chunkCount,
		"uniqueChecksums": len(checksums),
	}, "H-A")
	// #endregion

	if streamCount == 0 {
		fmt.Println("\nWARNING: no streams found")
	}
}

func resolveSections(idx *tsdb.TSDBIndex, tenant string, matchers []*labels.Matcher) {
	refs, err := idx.GetDataobjSections(
		context.Background(), tenant,
		model.Earliest, model.Latest,
		nil,
		matchers...,
	)
	exitErr("GetDataobjSections", err)

	// #region agent log
	debugLog("resolveSections", "result", map[string]interface{}{
		"sectionsResolved": len(refs),
	}, "H-A")
	// #endregion

	if len(refs) == 0 {
		fmt.Println("no sections resolved")
		return
	}

	uniquePaths := make(map[string]struct{})
	totalStreamIDs := 0

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "PATH\tSECTION\tMIN_TIME\tMAX_TIME\tKB\tENTRIES\tSTREAM_IDS\n")
	fmt.Fprintf(w, "----\t-------\t--------\t--------\t--\t-------\t----------\n")
	for _, ref := range refs {
		uniquePaths[ref.Path] = struct{}{}
		totalStreamIDs += len(ref.StreamIDs)
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			ref.Path, ref.SectionID,
			int64(ref.MinTime), int64(ref.MaxTime),
			ref.KB, ref.Entries, len(ref.StreamIDs),
		)
	}
	w.Flush()

	fmt.Printf("\nsummary: %d sections across %d unique objects, %d total stream IDs\n",
		len(refs), len(uniquePaths), totalStreamIDs)
}

func parseMatchers(input string) ([]*labels.Matcher, error) {
	if input == "" || input == "{}" {
		return nil, nil
	}
	return syntax.ParseMatchers(input, false)
}

func decompressToTemp(gzPath, pattern string) (*os.File, error) {
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

	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, err
	}

	n, err := io.Copy(tmp, gz)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("decompress (%d bytes written): %w", n, err)
	}

	fmt.Printf("  %s -> %s (%s)\n", gzPath, tmp.Name(), humanBytes(n))
	tmp.Close()
	return tmp, nil
}

func cleanupTemp(f *os.File) {
	if f != nil {
		os.Remove(f.Name())
	}
}

func exitErr(ctx string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error %s: %v\n", ctx, err)
		os.Exit(1)
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
