//go:build unix

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"syscall"
)

func main() {
	var (
		pkg       = flag.String("pkg", "", "Go package whose test binary contains the benchmarks (e.g. ./pkg/logql/)")
		benchArg  = flag.String("bench", "", "comma-separated benchmark name(s) to measure; each is matched exactly, per sub-benchmark level")
		count     = flag.Int("count", 5, "number of processes to run per benchmark (samples); the reported value is the median")
		benchtime = flag.String("benchtime", "1x", "value passed to -test.benchtime")
		warmup    = flag.Bool("warmup", true, "run each benchmark once before measuring, to warm on-disk caches / fixtures so one-time generation memory is excluded")
	)

	flag.Parse()

	if *pkg == "" || *benchArg == "" {
		fmt.Fprintln(os.Stderr, "both -pkg and -bench are required")
		flag.Usage()
		os.Exit(2)
	}

	var benchmarks []string
	for _, b := range strings.Split(*benchArg, ",") {
		if b = strings.TrimSpace(b); b != "" {
			benchmarks = append(benchmarks, b)
		}
	}
	if len(benchmarks) == 0 {
		fatal(errors.New("no benchmarks given in -bench"))
	}

	binary, cleanup, err := buildTestBinary(*pkg)
	if err != nil {
		fatal(fmt.Errorf("building test binary for %s: %w", *pkg, err))
	}
	defer cleanup()

	// Run the benchmarks and keep track of the peak RSS byte samples for each one.
	samples := map[string][]int64{}
	for _, b := range benchmarks {
		if *warmup {
			fmt.Fprintf(os.Stderr, "warming %s (fixtures/caches)...\n", b)
			if _, err := runBenchmark(binary, b, *benchtime); err != nil {
				fatal(fmt.Errorf("warming %s: %w", b, err))
			}
		}
		for i := 0; i < *count; i++ {
			rss, err := runBenchmark(binary, b, *benchtime)
			if err != nil {
				fatal(fmt.Errorf("running %s: %w", b, err))
			}
			samples[b] = append(samples[b], rss)

			// Write the output in a benchstat-parseable format:
			//<name>-<cpus> <iters> <value> <unit>
			fmt.Printf("%s-%d %d %d peak-RSS-bytes/op\n", b, runtime.NumCPU(), 1, rss)
		}
	}

	printResults(benchmarks, samples)
}

// buildTestBinary compiles the package's test binary once, so per-benchmark subprocesses do not
// include compilation memory in their RSS.
func buildTestBinary(pkg string) (string, func(), error) {
	f, err := os.CreateTemp("", "memory-peak-bench-*.test")
	if err != nil {
		return "", nil, err
	}
	path := f.Name()
	_ = f.Close()

	cmd := exec.Command("go", "test", "-c", "-o", path, pkg)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(path)
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// runBenchmark runs a single benchmark (and only that one) in its own process and returns its peak RSS
// in bytes. Every "/"-separated path element is anchored so the -bench pattern matches exactly the
// requested (sub-)benchmark and nothing else.
func runBenchmark(bin, bench, benchtime string) (int64, error) {
	parts := strings.Split(bench, "/")
	for i := range parts {
		parts[i] = "^" + parts[i] + "$"
	}
	pattern := strings.Join(parts, "/")

	cmd := exec.Command(bin,
		"-test.run=^$",
		"-test.bench="+pattern,
		"-test.benchtime="+benchtime,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return 0, err
	}

	usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0, fmt.Errorf("no rusage available for %s", bench)
	}
	return maxRSSBytes(usage), nil
}

// maxRSSBytes normalizes Rusage.Maxrss to bytes (kilobytes on Linux, bytes on macOS).
func maxRSSBytes(u *syscall.Rusage) int64 {
	if runtime.GOOS == "darwin" {
		return u.Maxrss
	}
	return u.Maxrss * 1024
}

// printResults prints the median peak RSS per benchmark, in input order. When exactly two
// benchmarks are given, it also prints their ratio (first/second) — handy for A/B comparisons.
func printResults(benches []string, samples map[string][]int64) {
	width := 0
	for _, b := range benches {
		if len(b) > width {
			width = len(b)
		}
	}

	fmt.Println()
	fmt.Println("peak RSS (median of samples), lower is better:")
	for _, b := range benches {
		fmt.Printf("%-*s  %12s\n", width, b, humanize(median(samples[b])))
	}

	if len(benches) == 2 {
		first, second := median(samples[benches[0]]), median(samples[benches[1]])
		if second > 0 {
			fmt.Printf("\nratio (first/second): %.2fx\n", float64(first)/float64(second))
		}
	}
}

func median(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func humanize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGT"[exp])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
