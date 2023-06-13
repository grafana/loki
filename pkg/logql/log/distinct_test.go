package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

func Test_DistinctFilter(t *testing.T) {

	c := struct {
		name          string
		label         []string
		lbs           labels.Labels
		input         []string
		expectedCount int
		expectedLines []string
	}{
		name:  "distinct test",
		label: []string{"id", "time", "none"},
		lbs: labels.FromStrings(logqlmodel.ErrorLabel, errJSON,
			"status", "200",
			"method", "POST",
		),
		input: []string{
			`{"event": "access", "id": "1", "time": "1"}`,
			`{"event": "access", "id": "1", "time": "2"}`,
			`{"event": "access", "id": "2", "time": "3"}`,
			`{"event": "access", "id": "2", "time": "4"}`,
			`{"event": "access", "id": "1", "time": "5"}`,
			`{"event": "delete", "id": "1", "time": "1"}`,
		},
		expectedCount: 2,
		expectedLines: []string{
			`{"event": "access", "id": "1", "time": "1"}`,
			`{"event": "access", "id": "2", "time": "3"}`,
		},
	}

	distinctFilter, err := NewDistinctFilter(c.label)
	require.NoError(t, err)

	total := 0
	lines := make([]string, 0)
	b := NewBaseLabelsBuilder().ForLabels(c.lbs, c.lbs.Hash())
	for _, line := range c.input {
		NewJSONParser().Process(1, []byte(line), b)
		_, ok := distinctFilter.Process(1, []byte(line), b)
		if ok {
			total++
			lines = append(lines, line)
		}
	}

	require.Equal(t, c.expectedCount, total)
	require.Equal(t, c.expectedLines, lines)

}
