package otlpattrs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"
)

func attrs(kv ...string) push.LabelsAdapter {
	adapter := make(push.LabelsAdapter, 0, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		adapter = append(adapter, push.LabelAdapter{Name: kv[i], Value: kv[i+1]})
	}
	return adapter
}

func TestAccumulatorObserve(t *testing.T) {
	acc := NewAccumulator()

	// "cluster"+"prod" is 11 bytes on each of 3 records.
	acc.Observe(KindResource, attrs("cluster", "prod"), 3)
	// Same attribute again from a second resource block, with a longer value.
	acc.Observe(KindResource, attrs("cluster", "staging"), 2)
	acc.Observe(KindScope, attrs("scope_name", "otelcol"), 5)

	report := acc.Report(0)
	require.Equal(t, 2, report.Attributes)
	require.Len(t, report.Top, 2)

	byName := map[string]Attribute{}
	for _, attr := range report.Top {
		byName[string(attr.Kind)+"."+attr.Name] = attr
	}

	cluster := byName["resource.cluster"]
	require.Equal(t, int64(5), cluster.Records)
	require.Equal(t, int64(3*11+2*14), cluster.ExpandedBytes)

	scope := byName["scope.scope_name"]
	require.Equal(t, int64(5), scope.Records)
	require.Equal(t, int64(5*17), scope.ExpandedBytes)
}

func TestReportRanksAndTruncates(t *testing.T) {
	acc := NewAccumulator()
	// Give each attribute a distinct value length so the ranking is unambiguous.
	for i := range 10 {
		acc.Observe(KindResource, attrs(fmt.Sprintf("attr_%02d", i), string(make([]byte, i))), 1)
	}

	report := acc.Report(3)
	require.Equal(t, 10, report.Attributes)

	require.Len(t, report.Top, 3)
	require.Equal(t, []string{"attr_09", "attr_08", "attr_07"}, []string{report.Top[0].Name, report.Top[1].Name, report.Top[2].Name})
	require.Greater(t, report.Top[0].ExpandedBytes, report.Top[1].ExpandedBytes)

	require.Equal(t, 7, report.OverflowAttributes)
	require.Equal(t, []string{"attr_06", "attr_05", "attr_04", "attr_03", "attr_02", "attr_01", "attr_00"},
		report.OverflowNames)

	// Every observed byte is accounted for, either individually or in the overflow.
	var topBytes int64
	for _, attr := range report.Top {
		topBytes += attr.ExpandedBytes
	}
	var expectedTotal int64
	for i := range 10 {
		expectedTotal += int64(len("attr_00") + i)
	}
	require.Equal(t, expectedTotal, topBytes+report.OverflowExpandedBytes)
}
