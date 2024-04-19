package stages

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	dockerRaw       = `{"log":"level=info ts=2019-04-30T02:12:41.844179Z caller=filetargetmanager.go:180 msg=\"Adding target\" key=\"{com_docker_deploy_namespace=\\\"docker\\\", com_docker_fry=\\\"compose.api\\\", com_docker_image_tag=\\\"v0.4.12\\\", container_name=\\\"compose\\\", instance=\\\"compose-api-cbff6dfc9-cqfr8\\\", job=\\\"docker/compose-api\\\", namespace=\\\"docker\\\", pod_template_hash=\\\"769928975\\\"}\"\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}`
	dockerProcessed = `level=info ts=2019-04-30T02:12:41.844179Z caller=filetargetmanager.go:180 msg="Adding target" key="{com_docker_deploy_namespace=\"docker\", com_docker_fry=\"compose.api\", com_docker_image_tag=\"v0.4.12\", container_name=\"compose\", instance=\"compose-api-cbff6dfc9-cqfr8\", job=\"docker/compose-api\", namespace=\"docker\", pod_template_hash=\"769928975\"}"
`
	dockerInvalidTimestampRaw = `{"log":"log message\n","stream":"stderr","time":"hi!"}`
	dockerTestTimeNow         = time.Now()
)

func TestNewDocker(t *testing.T) {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatal("could not parse timezone", err)
	}

	tests := map[string]struct {
		entry          string
		expectedEntry  string
		t              time.Time
		expectedT      time.Time
		labels         map[string]string
		expectedLabels map[string]string
	}{
		"happy path": {
			dockerRaw,
			dockerProcessed,
			time.Now(),
			time.Date(2019, 4, 30, 02, 12, 41, 844351500, loc),
			map[string]string{},
			map[string]string{
				"stream": "stderr",
			},
		},
		"invalid timestamp": {
			dockerInvalidTimestampRaw,
			"log message\n",
			dockerTestTimeNow,
			dockerTestTimeNow,
			map[string]string{},
			map[string]string{
				"stream": "stderr",
			},
		},
		"invalid json": {
			"i'm not json!",
			"i'm not json!",
			dockerTestTimeNow,
			dockerTestTimeNow,
			map[string]string{},
			map[string]string{},
		},
	}

	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			p, err := NewDocker(util_log.Logger, prometheus.DefaultRegisterer)
			if err != nil {
				t.Fatalf("failed to create Docker parser: %s", err)
			}
			out := processEntries(p, newEntry(nil, toLabelSet(tt.labels), tt.entry, tt.t))[0]

			assertLabels(t, tt.expectedLabels, out.Labels)
			assert.Equal(t, tt.expectedEntry, out.Line, "did not receive expected log entry")
			if out.Timestamp.Unix() != tt.expectedT.Unix() {
				t.Fatalf("mismatch ts want: %s got:%s", tt.expectedT, tt.t)
			}
		})
	}
}

var (
	criTestTimeStr = "2019-01-01T01:00:00.000000001Z"
	criTestTime, _ = time.Parse(time.RFC3339Nano, criTestTimeStr)
	criTestTime2   = time.Now()
)

type testEntry struct {
	labels model.LabelSet
	line   string
}

func TestCRI_tags(t *testing.T) {
	cases := []struct {
		name                       string
		lines                      []string
		expected                   []string
		maxPartialLines            int
		maxPartialLineSize         int
		maxPartialLineSizeTruncate bool
		entries                    []testEntry
		err                        error
	}{
		{
			name: "tag F",
			entries: []testEntry{
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout F some full line", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F log", labels: model.LabelSet{"foo": "bar"}},
			},
			expected: []string{"some full line", "log"},
		},
		{
			name: "tag P multi-stream",
			entries: []testEntry{
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 1 ", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 2 ", labels: model.LabelSet{"foo": "bar2"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F log finished", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F another full log", labels: model.LabelSet{"foo": "bar2"}},
			},
			expected: []string{
				"partial line 1 log finished",     // belongs to stream `{foo="bar"}`
				"partial line 2 another full log", // belongs to stream `{foo="bar2"}
			},
		},
		{
			name: "tag P multi-stream with maxPartialLines exceeded",
			entries: []testEntry{
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 1 ", labels: model.LabelSet{"label1": "val1", "label2": "val2"}},

				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 2 ", labels: model.LabelSet{"label1": "val1"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 3 ", labels: model.LabelSet{"label1": "val1", "label2": "val2"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 4 ", labels: model.LabelSet{"label1": "val3"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 5 ", labels: model.LabelSet{"label1": "val4"}}, // exceeded maxPartialLines as already 3 streams in flight.
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F log finished", labels: model.LabelSet{"label1": "val1", "label2": "val2"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F another full log", labels: model.LabelSet{"label1": "val3"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F yet an another full log", labels: model.LabelSet{"label1": "val4"}},
			},
			maxPartialLines: 3,
			expected: []string{
				"partial line 1 partial line 3 ",
				"partial line 2 ",
				"partial line 4 ",
				"log finished",
				"another full log",
				"partial line 5 yet an another full log",
			},
		},
		{
			name: "tag P single stream",
			entries: []testEntry{
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 1 ", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 2 ", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 3 ", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 4 ", labels: model.LabelSet{"foo": "bar"}}, // this exceeds the `MaxPartialLinesSize` of 3
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F log finished", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F another full log", labels: model.LabelSet{"foo": "bar"}},
			},
			maxPartialLines: 3,
			expected: []string{
				"partial line 1 partial line 2 partial line 3 partial line 4 log finished",
				"another full log",
			},
		},
		{
			name: "tag P multi-stream with truncation",
			entries: []testEntry{
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial line 1 ", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:50.904275087+00:00 stdout P partial", labels: model.LabelSet{"foo": "bar2"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F log finished", labels: model.LabelSet{"foo": "bar"}},
				{line: "2019-05-07T18:57:55.904275087+00:00 stdout F full", labels: model.LabelSet{"foo": "bar2"}},
			},
			maxPartialLineSizeTruncate: true,
			maxPartialLineSize:         11,
			expected: []string{
				"partial lin",
				"partialfull",
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := map[string]interface{}{
				"max_partial_lines":              tt.maxPartialLines,
				"max_partial_line_size":          tt.maxPartialLineSize,
				"max_partial_line_size_truncate": tt.maxPartialLineSizeTruncate,
			}
			p, err := NewCRI(util_log.Logger, cfg, prometheus.DefaultRegisterer)
			require.NoError(t, err)

			got := make([]string, 0)

			for _, entry := range tt.entries {
				out := processEntries(p, newEntry(nil, entry.labels, entry.line, time.Now()))
				if len(out) > 0 {
					for _, en := range out {
						got = append(got, en.Line)
					}
				}
			}

			expectedMap := make(map[string]bool)
			for _, v := range tt.expected {
				expectedMap[v] = true
			}

			gotMap := make(map[string]bool)
			for _, v := range got {
				gotMap[v] = true
			}

			assert.Equal(t, expectedMap, gotMap)
		})
	}
}

func TestNewCri(t *testing.T) {
	tests := map[string]struct {
		entry          string
		expectedEntry  string
		t              time.Time
		expectedT      time.Time
		labels         map[string]string
		expectedLabels map[string]string
	}{
		"happy path": {
			criTestTimeStr + " stderr F message",
			"message",
			time.Now(),
			criTestTime,
			map[string]string{},
			map[string]string{
				"stream": "stderr",
			},
		},
		"multi line pass": {
			criTestTimeStr + " stderr F message\nmessage2",
			"message\nmessage2",
			time.Now(),
			criTestTime,
			map[string]string{},
			map[string]string{
				"stream": "stderr",
			},
		},
		"invalid timestamp": {
			"3242 stderr F message",
			"message",
			criTestTime2,
			criTestTime2,
			map[string]string{},
			map[string]string{
				"stream": "stderr",
			},
		},
		"invalid line": {
			"i'm invalid!!!",
			"i'm invalid!!!",
			criTestTime2,
			criTestTime2,
			map[string]string{},
			map[string]string{},
		},
	}

	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			cfg := map[string]interface{}{}
			p, err := NewCRI(util_log.Logger, cfg, prometheus.DefaultRegisterer)
			if err != nil {
				t.Fatalf("failed to create CRI parser: %s", err)
			}
			out := processEntries(p, newEntry(nil, toLabelSet(tt.labels), tt.entry, tt.t))[0]

			assertLabels(t, tt.expectedLabels, out.Labels)
			assert.Equal(t, tt.expectedEntry, out.Line, "did not receive expected log entry")
			if out.Timestamp.Unix() != tt.expectedT.Unix() {
				t.Fatalf("mismatch ts want: %s got:%s", tt.expectedT, tt.t)
			}
		})
	}

}
