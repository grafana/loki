package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// benchOutput is a realistic `go test -bench BenchmarkLogQLMetricQueries` run: a range query with
// all three scenarios (including a duplicated chunk-timestamp-first/latency=0s line, to cover
// averaging duplicate samples) and an instant query that only runs chunk-timestamp-first.
const benchOutput = `goos: darwin
goarch: arm64
pkg: github.com/grafana/loki/v3/pkg/logql/internal/logqlbench
cpu: Apple M3 Pro
BenchmarkLogQLMetricQueries
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-timestamp-first/latency=0s-11         	       1	1000000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	1 store_max_parallel	2000000 B/op	500000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-timestamp-first/latency=0s-11         	       1	3000000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	1 store_max_parallel	2000000 B/op	500000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-timestamp-first/latency=50ms-11        	       1	1100000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	2 store_max_parallel	2000000 B/op	500000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-timestamp-first/latency=250ms-11       	       1	1300000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	8 store_max_parallel	2000000 B/op	500000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-stream-first/latency=0s-11            	       1	500000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	1 store_max_parallel	1000000 B/op	250000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-stream-first/latency=50ms-11          	       1	550000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	2 store_max_parallel	1000000 B/op	250000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=chunk-stream-first/latency=250ms-11         	       1	700000000 ns/op	 1000000 store_bytes/op	      10 store_reqs/op	16 store_max_parallel	1000000 B/op	250000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=dataobj-stream-first/latency=0s-11          	       1	100000000 ns/op	 200000 store_bytes/op	      5 store_reqs/op	1 store_max_parallel	300000 B/op	50000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=dataobj-stream-first/latency=50ms-11        	       1	900000000 ns/op	 200000 store_bytes/op	      5 store_reqs/op	2 store_max_parallel	300000 B/op	50000 allocs/op
BenchmarkLogQLMetricQueries/query=sum(count_over_time({cluster="x"}[5m]))_(high_input,_low_output,_range)/scenario=dataobj-stream-first/latency=250ms-11       	       1	4000000000 ns/op	 200000 store_bytes/op	      5 store_reqs/op	32 store_max_parallel	300000 B/op	50000 allocs/op
BenchmarkLogQLMetricQueries/query=count_over_time({service="y"}[24h])_(medium_input,_medium_output,_instant)/scenario=chunk-timestamp-first/latency=0s-11      	       1	2000000000 ns/op	 5000000 store_bytes/op	      20 store_reqs/op	1 store_max_parallel	4000000 B/op	1000000 allocs/op
BenchmarkLogQLMetricQueries/query=count_over_time({service="y"}[24h])_(medium_input,_medium_output,_instant)/scenario=chunk-timestamp-first/latency=50ms-11    	       1	2100000000 ns/op	 5000000 store_bytes/op	      20 store_reqs/op	2 store_max_parallel	4000000 B/op	1000000 allocs/op
BenchmarkLogQLMetricQueries/query=count_over_time({service="y"}[24h])_(medium_input,_medium_output,_instant)/scenario=chunk-timestamp-first/latency=250ms-11   	       1	2300000000 ns/op	 5000000 store_bytes/op	      20 store_reqs/op	4 store_max_parallel	4000000 B/op	1000000 allocs/op
PASS
ok  	github.com/grafana/loki/v3/pkg/logql/internal/logqlbench	12.345s
`

const wantReport = "## Benchmark results\n" +
	"\n" +
	"Each cell lists one value per scenario, in the order:\n" +
	"\n" +
	"- chunk-timestamp-first\n" +
	"- chunk-stream-first\n" +
	"- dataobj-stream-first\n" +
	"\n" +
	"| Query (shape) | Type | latency=0s | latency=50ms | latency=250ms |\n" +
	"|---|---|---|---|---|\n" +
	"| `sum(count over time({cluster=\"x\"}[5m]))`<br>high input, low output | range | 2.00s<br>0.50s<br>0.10s | 1.10s<br>0.55s<br>0.90s | 1.30s<br>0.70s<br>4.00s |\n" +
	"| `count over time({service=\"y\"}[24h])`<br>medium input, medium output | instant | 2.00s<br>n/a<br>n/a | 2.10s<br>n/a<br>n/a | 2.30s<br>n/a<br>n/a |\n" +
	"\n" +
	"| Query (shape) | Type | Store ops | Bytes fetched from storage | Max parallel reads | Memory allocations | Memory operations (bytes) |\n" +
	"|---|---|---|---|---|---|---|\n" +
	"| `sum(count over time({cluster=\"x\"}[5m]))`<br>high input, low output | range | 10<br>10<br>5 | 1.0MB<br>1.0MB<br>0.2MB | 8<br>16<br>32 | 0.50M<br>0.25M<br>0.05M | 2.0MB<br>1.0MB<br>0.3MB |\n" +
	"| `count over time({service=\"y\"}[24h])`<br>medium input, medium output | instant | 20<br>n/a<br>n/a | 5.0MB<br>n/a<br>n/a | 4<br>n/a<br>n/a | 1.00M<br>n/a<br>n/a | 4.0MB<br>n/a<br>n/a |\n"

// gomaxprocs1RE strips a GOMAXPROCS suffix ("-11") from a benchmark line, matching the output of
// a `go test -cpu 1` run: Go omits that suffix when GOMAXPROCS is 1.
var gomaxprocs1RE = regexp.MustCompile(`(latency=[0-9a-z]+)-\d+(\s)`)

func TestGenerate(t *testing.T) {
	t.Run("a full benchmark run produces the expected markdown tables", func(t *testing.T) {
		rep, err := parse(strings.NewReader(benchOutput))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		var buf bytes.Buffer
		generate(&buf, rep)

		if got := buf.String(); got != wantReport {
			t.Errorf("generate() output mismatch\ngot:\n%s\nwant:\n%s", got, wantReport)
		}
	})

	t.Run("a GOMAXPROCS=1 run, which omits the -N suffix, produces the same markdown tables", func(t *testing.T) {
		input := gomaxprocs1RE.ReplaceAllString(benchOutput, "$1$2")

		rep, err := parse(strings.NewReader(input))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		var buf bytes.Buffer
		generate(&buf, rep)

		if got := buf.String(); got != wantReport {
			t.Errorf("generate() output mismatch\ngot:\n%s\nwant:\n%s", got, wantReport)
		}
	})
}
