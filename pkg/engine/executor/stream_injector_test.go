package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func Test_streamInjector(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	inputStreams := []labels.Labels{
		labels.FromStrings("app", "loki", "env", "prod", "region", "us-west"),
		labels.FromStrings("app", "loki", "env", "dev"),
		labels.FromStrings("app", "loki", "env", "prod", "region", "us-east"),
	}

	sec := buildStreamsSection(t, inputStreams)

	input := arrowtest.Rows{
		{"stream_id.int64": 2, "ts": int64(1), "line": "log line 1"},
		{"stream_id.int64": 1, "ts": int64(2), "line": "log line 2"},
		{"stream_id.int64": 3, "ts": int64(3), "line": "log line 3"},
		{"stream_id.int64": 2, "ts": int64(4), "line": "log line 4"},
	}

	record := input.Record(alloc, input.Schema())
	defer record.Release()

	view := newStreamsView(sec, &streamsViewOptions{})
	defer view.Close()

	injector := newStreamInjector(alloc, view)
	output, err := injector.Inject(t.Context(), record)
	if output != nil {
		defer output.Release()
	}
	require.NoError(t, err)

	expect := arrowtest.Rows{
		{"app": "loki", "env": "dev", "region": nil, "ts": int64(1), "line": "log line 1"},
		{"app": "loki", "env": "prod", "region": "us-west", "ts": int64(2), "line": "log line 2"},
		{"app": "loki", "env": "prod", "region": "us-east", "ts": int64(3), "line": "log line 3"},
		{"app": "loki", "env": "dev", "region": nil, "ts": int64(4), "line": "log line 4"},
	}

	actual, err := arrowtest.RecordRows(output)
	require.NoError(t, err, "failed to convert output record to rows")
	require.Equal(t, expect, actual, "expected output to match input with stream labels injected")
}
