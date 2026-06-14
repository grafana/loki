package commands

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

func TestCheckDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []rwrulefmt.RuleGroup
		want []compareRuleType
	}{
		{
			name: "no duplicates",
			in: []rwrulefmt.RuleGroup{{
				RuleGroup: rulefmt.RuleGroup{
					Name: "rulegroup",
					Rules: []rulefmt.Rule{
						{
							Record: "up",
							Expr:   "up==1",
						},
						{
							Record: "down",
							Expr:   "up==0",
						},
					},
				},
				RWConfigs: []rwrulefmt.RemoteWriteConfig{},
			}},
			want: nil,
		},
		{
			name: "with duplicates",
			in: []rwrulefmt.RuleGroup{{
				RuleGroup: rulefmt.RuleGroup{
					Name: "rulegroup",
					Rules: []rulefmt.Rule{
						{
							Record: "up",
							Expr:   "up==1",
						},
						{
							Record: "up",
							Expr:   "up==0",
						},
					},
				},
				RWConfigs: []rwrulefmt.RemoteWriteConfig{},
			}},
			want: []compareRuleType{{metric: "up", label: map[string]string(nil)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, checkDuplicates(tc.in))
		})
	}
}

func TestRuleCommandHelpClarifiesFormatAndCheck(t *testing.T) {
	app := kingpin.New("lokitool", "A command-line tool to manage Loki.")
	var ruleCommand RuleCommand
	ruleCommand.Register(app)

	t.Run("rules help", func(t *testing.T) {
		ctx, err := app.ParseContext([]string{"rules"})
		require.NoError(t, err)

		var buf bytes.Buffer
		app.Writer(&buf)
		require.NoError(t, app.UsageForContext(ctx))

		out := buf.String()
		assert.Contains(t, out, "rules format [<flags>] [<rule-files>...]")
		assert.Contains(t, out, "The `lint`")
		assert.Contains(t, out, "alias is deprecated and kept only for backward compatibility.")
		assert.Contains(t, out, "rules check [<flags>] [<rule-files>...]")
		assert.Contains(t, out, "Lints rules against best-practice checks without rewriting the files.")
	})

	t.Run("format help", func(t *testing.T) {
		ctx, err := app.ParseContext([]string{"rules", "format"})
		require.NoError(t, err)

		var buf bytes.Buffer
		app.Writer(&buf)
		require.NoError(t, app.UsageForContext(ctx))

		out := buf.String()
		assert.Contains(t, out, "The `lint` alias")
		assert.Contains(t, out, "is deprecated and kept only for backward compatibility.")
		assert.Contains(t, out, "The rule files to format.")
	})
}
