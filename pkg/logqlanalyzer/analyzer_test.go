package logqlanalyzer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_logQLAnalyzer_analyze_stages(t *testing.T) {
	tests := map[string]struct {
		query                  string
		expectedStreamSelector string
		expectedStages         []string
	}{
		"expected 2 stages and streamSelector to be detected": {
			query:                  "{job=\"analyze\"} | json |= \"info\"",
			expectedStreamSelector: "{job=\"analyze\"}",
			expectedStages: []string{
				"| json",
				"|= \"info\"",
			},
		},
		"expected 2 stages and streamSelector to be detected even if query contains 4 stages": {
			query:                  "{job=\"analyze\"} | pattern \"<_> <level> <msg>\" |= \"info\" |~ \"some_expr\"",
			expectedStreamSelector: "{job=\"analyze\"}",
			expectedStages: []string{
				"| pattern \"<_> <level> <msg>\"",
				"|= \"info\" |~ \"some_expr\"",
			},
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := logQLAnalyzer{}.analyze(data.query, []string{})
			require.NoError(t, err)
			require.Equal(t, data.expectedStreamSelector, result.StreamSelector)
			require.Equal(t, data.expectedStages, result.Stages)
		})
	}
}

func Test_logQLAnalyzer_analyze_expected_1_stage_record_for_each_log_line(t *testing.T) {
	line0 := "lvl=error msg=a"
	line1 := "lvl=info msg=b"

	result, err := logQLAnalyzer{}.analyze("{job=\"analyze\"} | logfmt", []string{line0, line1})

	require.NoError(t, err)
	require.Equal(t, 2, len(result.Results))
	require.Equal(t, 1, len(result.Results[0].StageRecords))
	require.Equal(t, 1, len(result.Results[1].StageRecords))
}

func Test_logQLAnalyzer_analyze_expected_all_stage_records_to_be_correct(t *testing.T) {
	line := "lvl=error msg=a"
	reformattedLine := "level=error message=A"
	result, err := logQLAnalyzer{}.analyze("{job=\"analyze\"} | logfmt | line_format \"level={{.lvl}} message={{.msg | ToUpper}}\" |= \"info\"", []string{line})
	require.NoError(t, err)
	require.Equal(t, 1, len(result.Results))
	require.Equal(t, 3, len(result.Results[0].StageRecords), "expected records for two stages")
	streamLabels := []Label{{"job", "analyze"}}
	parsedLabels := append(streamLabels, []Label{{"lvl", "error"}, {"msg", "a"}}...)
	require.Equal(t, StageRecord{
		LineBefore:   line,
		LabelsBefore: streamLabels,
		LineAfter:    line,
		LabelsAfter:  parsedLabels,
		FilteredOut:  false,
	}, result.Results[0].StageRecords[0])
	require.Equal(t, StageRecord{
		LineBefore:   line,
		LabelsBefore: parsedLabels,
		LineAfter:    reformattedLine,
		LabelsAfter:  parsedLabels,
		FilteredOut:  false,
	}, result.Results[0].StageRecords[1], "line is expected to be reformatted on this stage")
	require.Equal(t, StageRecord{
		LineBefore:   reformattedLine,
		LabelsBefore: parsedLabels,
		LineAfter:    reformattedLine,
		LabelsAfter:  parsedLabels,
		FilteredOut:  true,
	}, result.Results[0].StageRecords[2], "line is expected to be filtered out on this stage")
}
