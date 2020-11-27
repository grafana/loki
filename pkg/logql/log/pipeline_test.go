package log

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestNoopPipeline(t *testing.T) {
	lbs := labels.Labels{{Name: "foo", Value: "bar"}}
	l, lbr, ok := NewNoopPipeline().ForStream(lbs).Process([]byte(""))
	require.Equal(t, []byte(""), l)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, ok)

	ls, lbr, ok := NewNoopPipeline().ForStream(lbs).ProcessString("")
	require.Equal(t, "", ls)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, ok)
}

func TestPipeline(t *testing.T) {
	lbs := labels.Labels{{Name: "foo", Value: "bar"}}
	p := NewPipeline([]Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		newMustLineFormatter("lbs {{.foo}}"),
	})
	l, lbr, ok := p.ForStream(lbs).Process([]byte("line"))
	require.Equal(t, []byte("lbs bar"), l)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, ok)

	ls, lbr, ok := p.ForStream(lbs).ProcessString("line")
	require.Equal(t, "lbs bar", ls)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, ok)

	l, lbr, ok = p.ForStream(labels.Labels{}).Process([]byte("line"))
	require.Equal(t, []byte(nil), l)
	require.Equal(t, nil, lbr)
	require.Equal(t, false, ok)

	ls, lbr, ok = p.ForStream(labels.Labels{}).ProcessString("line")
	require.Equal(t, "", ls)
	require.Equal(t, nil, lbr)
	require.Equal(t, false, ok)
}

var (
	resOK   bool
	resLine []byte
	resLbs  LabelsResult
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
	sp := p.ForStream(lbs)
	for n := 0; n < b.N; n++ {
		resLine, resLbs, resOK = sp.Process(line)
	}

}

func mustFilter(f Filterer, err error) Filterer {
	if err != nil {
		panic(err)
	}
	return f
}
