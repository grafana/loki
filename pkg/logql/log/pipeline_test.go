package log

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	resOK   bool
	resLine []byte
	resLbs  labels.Labels
)

func Benchmark_Pipeline(b *testing.B) {
	b.ReportAllocs()
	p := NewPipeline([]Stage{
		mustFilter(NewFilter("metrics.go", labels.MatchEqual)).ToStage(),
		NewLogfmtParser(),
		NewAndLabelFilter(
			NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 10*time.Millisecond),
			NewNumericLabelFilter(LabelFilterEqual, "status", 200.0),
		),
		mustNewLabelsFormatter([]LabelFmt{NewRenameLabelFmt("caller_foo", "caller"), NewTemplateLabelFmt("new", "{{.query_type}}:{{.range_type}}")}),
		NewJSONParser(),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, ErrorLabel, errJSON)),
		newMustLineFormatter("Q=>{{.query}},D=>{{.duration}}"),
	})
	line := []byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 org_id=29 traceID=29a0f088b047eb8c latency=fast query="{stream=\"stdout\",pod=\"loki-canary-xmjzp\"}" query_type=limited range_type=range length=20s step=1s duration=58.126671ms status=200 throughput_mb=2.496547 total_bytes_mb=0.145116`)
	lbs := labels.Labels{
		{Name: "cluster", Value: "ops-tool1"},
		{Name: "name", Value: "querier"},
		{Name: "pod", Value: "querier-5896759c79-q7q9h"},
		{Name: "stream", Value: "stderr"},
		{Name: "container", Value: "querier"},
		{Name: "namespace", Value: "loki-dev"},
		{Name: "job", Value: "loki-dev/querier"},
		{Name: "pod_template_hash", Value: "5896759c79"},
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		resLine, resLbs, resOK = p.Process(line, lbs)
	}

}

func mustFilter(f Filterer, err error) Filterer {
	if err != nil {
		panic(err)
	}
	return f
}
