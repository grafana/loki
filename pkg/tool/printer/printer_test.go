package printer

import (
	"bytes"
	"testing"

	"github.com/alecthomas/chroma/quick"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

func TestPrintRuleSet(t *testing.T) {
	giveRules := map[string][]rwrulefmt.RuleGroup{
		"test-namespace-1": {
			{RuleGroup: rulefmt.RuleGroup{Name: "test-rulegroup-a"}},
			{RuleGroup: rulefmt.RuleGroup{Name: "test-rulegroup-b"}},
		},
		"test-namespace-2": {
			{RuleGroup: rulefmt.RuleGroup{Name: "test-rulegroup-c"}},
			{RuleGroup: rulefmt.RuleGroup{Name: "test-rulegroup-d"}},
		},
	}

	wantJSONOutput := `[{"namespace":"test-namespace-1","rulegroup":"test-rulegroup-a"},{"namespace":"test-namespace-1","rulegroup":"test-rulegroup-b"},{"namespace":"test-namespace-2","rulegroup":"test-rulegroup-c"},{"namespace":"test-namespace-2","rulegroup":"test-rulegroup-d"}]`
	var wantColoredJSONBuffer bytes.Buffer
	err := quick.Highlight(&wantColoredJSONBuffer, wantJSONOutput, "json", "terminal", "swapoff")
	require.NoError(t, err)

	wantTabOutput := `Namespace        | Rule Group
test-namespace-1 | test-rulegroup-a
test-namespace-1 | test-rulegroup-b
test-namespace-2 | test-rulegroup-c
test-namespace-2 | test-rulegroup-d
`

	wantYAMLOutput := `- namespace: test-namespace-1
  rulegroup: test-rulegroup-a
- namespace: test-namespace-1
  rulegroup: test-rulegroup-b
- namespace: test-namespace-2
  rulegroup: test-rulegroup-c
- namespace: test-namespace-2
  rulegroup: test-rulegroup-d
`
	var wantColoredYAMLBuffer bytes.Buffer
	err = quick.Highlight(&wantColoredYAMLBuffer, wantYAMLOutput, "yaml", "terminal", "swapoff")
	require.NoError(t, err)

	tests := []struct {
		name             string
		giveDisableColor bool
		giveFormat       string
		wantOutput       string
	}{
		{
			name:             "prints colorless json",
			giveDisableColor: true,
			giveFormat:       "json",
			wantOutput:       wantJSONOutput,
		},
		{
			name:       "prints colorful json",
			giveFormat: "json",
			wantOutput: wantColoredJSONBuffer.String(),
		},
		{
			name:             "prints colorless yaml",
			giveDisableColor: true,
			giveFormat:       "yaml",
			wantOutput:       wantYAMLOutput,
		},
		{
			name:       "prints colorful yaml",
			giveFormat: "yaml",
			wantOutput: wantColoredYAMLBuffer.String(),
		},
		{
			name:             "defaults to tabwriter",
			giveDisableColor: true,
			wantOutput:       wantTabOutput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(tst *testing.T) {
			var b bytes.Buffer

			p := New(tt.giveDisableColor)
			err := p.PrintRuleSet(giveRules, tt.giveFormat, &b)

			require.NoError(tst, err)
			assert.Equal(tst, tt.wantOutput, b.String())
		})
	}
}
