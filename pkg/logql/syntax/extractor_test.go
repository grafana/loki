package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Extractor(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] offset 30s )`,
		`sum without(a) ( rate ( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum(count_over_time({job="mysql"}[5m]))`,
		`sum(count_over_time({job="mysql"} | json [5m]))`,
		`sum(count_over_time({job="mysql"} | logfmt [5m]))`,
		`sum(count_over_time({job="mysql"} | pattern "<foo> bar <buzz>" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m] offset 1h))`,
		`topk(10,sum(rate({region="us-east1"}[5m])) by (name))`,
		`topk by (name)(10,sum(rate({region="us-east1"}[5m])))`,
		`avg( rate( ( {job="nginx"} |= "GET" ) [10s] ) ) by (region)`,
		`avg(min_over_time({job="nginx"} |= "GET" | unwrap foo[10s])) by (region)`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m]))`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / sum by (cluster) (count_over_time({job="postgres"}[5m])) `,
		`
			sum by (cluster) (count_over_time({job="postgres"}[5m])) /
			sum by (cluster) (count_over_time({job="postgres"}[5m])) /
			sum by (cluster) (count_over_time({job="postgres"}[5m]))
			`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / min(count_over_time({job="mysql"}[5m])) `,
		`sum by (job) (
				count_over_time({namespace="tns"} |= "level=error"[5m])
			/
				count_over_time({namespace="tns"}[5m])
			)`,
		`stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m])`,
		`sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms|unwrap latency [5m])`,
		`sum by (job) (
				sum_over_time({namespace="tns"} |= "level=error" | json | foo=5 and bar<25ms | unwrap latency[5m])
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum by (job) (
				sum_over_time({namespace="tns"} |= "level=error" | json | foo=5 and bar<25ms | unwrap bytes(latency)[5m])
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum by (job) (
				sum_over_time(
					{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency) [5m]
				)
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json [5m])`,
		`sum by (job) (
				sum_over_time(
					{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m]
				)
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`label_replace(
				sum by (job) (
					sum_over_time(
						{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m]
					)
				/
					count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
				),
				"foo",
				"$1",
				"service",
				"(.*):.*"
			)
			`,
		`label_replace(
				sum by (job) (
					sum_over_time(
						{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m] offset 1h
					)
				/
					count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m] offset 1h)
				),
				"foo",
				"$1",
				"service",
				"(.*):.*"
			)
			`,
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseSampleExpr(tc)
			require.Nil(t, err)
			extractors, err := expr.Extractors()
			require.Nil(t, err)
			require.Len(t, extractors, 1)
		})
	}
}
