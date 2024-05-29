package docker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
)

type urlContainToPath struct {
	contains string
	filePath string
}

func handlerForPath(t *testing.T, paths []urlContainToPath, tty bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch path := r.URL.Path; {
		case strings.HasSuffix(path, "/logs"):
			var filePath string
			for _, cf := range paths {
				if strings.Contains(r.URL.RawQuery, cf.contains) {
					filePath = cf.filePath
					break
				}
			}
			assert.NotEmpty(t, filePath, "Did not find appropriate filePath to serve request")
			dat, err := os.ReadFile(filePath)
			require.NoError(t, err)
			_, err = w.Write(dat)
			require.NoError(t, err)
		default:
			w.Header().Set("Content-Type", "application/json")
			info := types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{},
				Mounts:            []types.MountPoint{},
				Config:            &container.Config{Tty: tty},
				NetworkSettings:   &types.NetworkSettings{},
			}
			err := json.NewEncoder(w).Encode(info)
			require.NoError(t, err)
		}
	})
}

func Test_DockerTarget(t *testing.T) {
	h := handlerForPath(t, []urlContainToPath{{"since=0", "testdata/flog.log"}, {"", "testdata/flog_after_restart.log"}}, false)

	ts := httptest.NewServer(h)
	defer ts.Close()

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	entryHandler := fake.New(func() {})
	client, err := client.NewClientWithOpts(client.WithHost(ts.URL))
	require.NoError(t, err)

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	require.NoError(t, err)

	target, err := NewTarget(
		NewMetrics(prometheus.NewRegistry()),
		logger,
		entryHandler,
		ps,
		"flog",
		model.LabelSet{"job": "docker"},
		[]*relabel.Config{},
		client,
		0,
	)
	require.NoError(t, err)

	expectedLines := []string{
		"5.3.69.55 - - [09/Dec/2021:09:15:02 +0000] \"HEAD /brand/users/clicks-and-mortar/front-end HTTP/2.0\" 503 27087",
		"101.54.183.185 - - [09/Dec/2021:09:15:03 +0000] \"POST /next-generation HTTP/1.0\" 416 11468",
		"69.27.137.160 - runolfsdottir2670 [09/Dec/2021:09:15:03 +0000] \"HEAD /content/visionary/engineer/cultivate HTTP/1.1\" 302 2975",
		"28.104.242.74 - - [09/Dec/2021:09:15:03 +0000] \"PATCH /value-added/cultivate/systems HTTP/2.0\" 405 11843",
		"150.187.51.54 - satterfield1852 [09/Dec/2021:09:15:03 +0000] \"GET /incentivize/deliver/innovative/cross-platform HTTP/1.1\" 301 13032",
	}

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assertExpectedLog(c, entryHandler, expectedLines)
	}, 5*time.Second, 100*time.Millisecond, "Expected log lines were not found within the time limit.")

	target.Stop()
	entryHandler.Clear()
	// restart target to simulate container restart
	target.startIfNotRunning()
	expectedLinesAfterRestart := []string{
		"243.115.12.215 - - [09/Dec/2023:09:16:57 +0000] \"DELETE /morph/exploit/granular HTTP/1.0\" 500 26468",
		"221.41.123.237 - - [09/Dec/2023:09:16:57 +0000] \"DELETE /user-centric/whiteboard HTTP/2.0\" 205 22487",
		"89.111.144.144 - - [09/Dec/2023:09:16:57 +0000] \"DELETE /open-source/e-commerce HTTP/1.0\" 401 11092",
		"62.180.191.187 - - [09/Dec/2023:09:16:57 +0000] \"DELETE /cultivate/integrate/technologies HTTP/2.0\" 302 12979",
		"156.249.2.192 - - [09/Dec/2023:09:16:57 +0000] \"POST /revolutionize/mesh/metrics HTTP/2.0\" 401 5297",
	}
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assertExpectedLog(c, entryHandler, expectedLinesAfterRestart)
	}, 5*time.Second, 100*time.Millisecond, "Expected log lines after restart were not found within the time limit.")
}

func doTestPartial(t *testing.T, tty bool) {
	var filePath string
	if tty {
		filePath = "testdata/partial-tty.log"
	} else {
		filePath = "testdata/partial.log"
	}
	h := handlerForPath(t, []urlContainToPath{{"", filePath}}, tty)

	ts := httptest.NewServer(h)
	defer ts.Close()

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	entryHandler := fake.New(func() {})
	client, err := client.NewClientWithOpts(client.WithHost(ts.URL))
	require.NoError(t, err)

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	require.NoError(t, err)

	target, err := NewTarget(
		NewMetrics(prometheus.NewRegistry()),
		logger,
		entryHandler,
		ps,
		"flog",
		model.LabelSet{"job": "docker"},
		[]*relabel.Config{},
		client,
		0,
	)
	require.NoError(t, err)

	expectedLines := []string{strings.Repeat("a", 16385)}
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assertExpectedLog(c, entryHandler, expectedLines)
	}, 10*time.Second, 100*time.Millisecond, "Expected log lines were not found within the time limit.")

	target.Stop()
	entryHandler.Clear()
}

func Test_DockerTargetPartial(t *testing.T) {
	doTestPartial(t, false)
}
func Test_DockerTargetPartialTty(t *testing.T) {
	doTestPartial(t, true)
}

// assertExpectedLog will verify that all expectedLines were received, in any order, without duplicates.
func assertExpectedLog(c *assert.CollectT, entryHandler *fake.Client, expectedLines []string) {
	logLines := entryHandler.Received()
	testLogLines := make(map[string]int)
	for _, l := range logLines {
		if containsString(expectedLines, l.Line) {
			testLogLines[l.Line]++
		}
	}
	// assert that all log lines were received
	assert.Len(c, testLogLines, len(expectedLines))
	// assert that there are no duplicated log lines
	for _, v := range testLogLines {
		assert.Equal(c, v, 1)
	}
}

func containsString(slice []string, str string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}
