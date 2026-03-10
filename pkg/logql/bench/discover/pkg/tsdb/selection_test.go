package tsdb

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// TestSelectWithDiversity_NoCapNeeded verifies that when len(seen) <= maxStreams
// all selectors are returned.
func TestSelectWithDiversity_NoCapNeeded(t *testing.T) {
	seen := map[string]loghttp.LabelSet{
		`{a="1"}`: {"a": "1"},
		`{b="2"}`: {"b": "2"},
	}
	result := selectWithDiversity(seen, 10)
	require.Len(t, result, 2)
	sort.Strings(result)
	require.Equal(t, []string{`{a="1"}`, `{b="2"}`}, result)
}

// TestSelectWithDiversity_DiversityAcrossServices verifies round-robin
// across service groups.
func TestSelectWithDiversity_DiversityAcrossServices(t *testing.T) {
	// 3 services × 4 streams each = 12 total, cap at 6.
	seen := make(map[string]loghttp.LabelSet)
	for _, svc := range []string{"alpha", "beta", "gamma"} {
		for i := 0; i < 4; i++ {
			key := fmt.Sprintf(`{instance="%d", service_name="%s"}`, i, svc)
			seen[key] = loghttp.LabelSet{"service_name": svc, "instance": fmt.Sprintf("%d", i)}
		}
	}

	result := selectWithDiversity(seen, 6)
	require.Len(t, result, 6)

	counts := make(map[string]int)
	for _, sel := range result {
		// Read service_name from the seen map directly.
		svc := seen[sel]["service_name"]
		counts[svc]++
	}
	require.Equal(t, 3, len(counts), "all 3 services should be represented")
	for svc, n := range counts {
		require.Equal(t, 2, n, "service %q should have exactly 2 streams", svc)
	}
}
