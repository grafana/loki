package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func Test_streamInjector(t *testing.T) {
	inputStreams := []labels.Labels{
		labels.FromStrings("app", "loki", "env", "prod", "region", "us-west"),
		labels.FromStrings("app", "loki", "env", "dev"),
		labels.FromStrings("app", "loki", "env", "prod", "region", "us-east"),
	}

	sec := buildStreamsSection(t, inputStreams)

	input := arrowtest.Rows{
		{streamInjectorColumnName: 2, "ts": int64(1), "line": "log line 1"},
		{streamInjectorColumnName: 1, "ts": int64(2), "line": "log line 2"},
		{streamInjectorColumnName: 3, "ts": int64(3), "line": "log line 3"},
		{streamInjectorColumnName: 2, "ts": int64(4), "line": "log line 4"},
	}

	record := input.Record(memory.DefaultAllocator, input.Schema())

	view := newStreamsView(sec, &streamsViewOptions{})
	defer view.Close()

	injector := newStreamInjector(view)
	output, err := injector.Inject(t.Context(), record)
	require.NoError(t, err)

	expect := arrowtest.Rows{
		{"utf8.label.app": "loki", "utf8.label.env": "dev", "utf8.label.region": nil, "ts": int64(1), "line": "log line 1"},
		{"utf8.label.app": "loki", "utf8.label.env": "prod", "utf8.label.region": "us-west", "ts": int64(2), "line": "log line 2"},
		{"utf8.label.app": "loki", "utf8.label.env": "prod", "utf8.label.region": "us-east", "ts": int64(3), "line": "log line 3"},
		{"utf8.label.app": "loki", "utf8.label.env": "dev", "utf8.label.region": nil, "ts": int64(4), "line": "log line 4"},
	}

	actual, err := arrowtest.RecordRows(output)
	require.NoError(t, err, "failed to convert output record to rows")
	require.Equal(t, expect, actual, "expected output to match input with stream labels injected")
}
