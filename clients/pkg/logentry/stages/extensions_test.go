package stages

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/pkg/util/log"
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

func TestCRI_tags(t *testing.T) {
	cases := []struct {
		name            string
		lines           []string
		expected        []string
		maxPartialLines int
		err             error
	}{
		{
			name: "tag F",
			lines: []string{
				"2019-05-07T18:57:50.904275087+00:00 stdout F some full line",
				"2019-05-07T18:57:55.904275087+00:00 stdout F log",
			},
			expected: []string{"some full line", "log"},
		},
		{
			name: "tag P",
			lines: []string{
				"2019-05-07T18:57:50.904275087+00:00 stdout P partial line 1 ",
				"2019-05-07T18:57:50.904275087+00:00 stdout P partial line 2 ",
				"2019-05-07T18:57:55.904275087+00:00 stdout F log finished",
				"2019-05-07T18:57:55.904275087+00:00 stdout F another full log",
			},
			expected: []string{
				"partial line 1 partial line 2 log finished",
				"another full log",
			},
		},
		{
			name: "tag P exceeding MaxPartialLinesSize lines",
			lines: []string{
				"2019-05-07T18:57:50.904275087+00:00 stdout P partial line 1 ",
				"2019-05-07T18:57:50.904275087+00:00 stdout P partial line 2 ",
				"2019-05-07T18:57:50.904275087+00:00 stdout P partial line 3",
				"2019-05-07T18:57:50.904275087+00:00 stdout P partial line 4 ", // this exceeds the `MaxPartialLinesSize` of 3
				"2019-05-07T18:57:55.904275087+00:00 stdout F log finished",
				"2019-05-07T18:57:55.904275087+00:00 stdout F another full log",
			},
			maxPartialLines: 3,
			expected: []string{
				"partial line 1 partial line 2 partial line 3",
				"partial line 4 log finished",
				"another full log",
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewCRI(util_log.Logger, prometheus.DefaultRegisterer)
			require.NoError(t, err)

			got := make([]string, 0)

			// tweak `maxPartialLines`
			if tt.maxPartialLines != 0 {
				p.(*cri).maxPartialLines = tt.maxPartialLines
			}

			for _, line := range tt.lines {
				out := processEntries(p, newEntry(nil, nil, line, time.Now()))
				if len(out) > 0 {
					for _, en := range out {
						got = append(got, en.Line)

					}
				}
			}
			assert.Equal(t, tt.expected, got)
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
			p, err := NewCRI(util_log.Logger, prometheus.DefaultRegisterer)
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
