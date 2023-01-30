package docker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
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
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
)

func Test_DockerTarget(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		switch path := r.URL.Path; {
		case strings.HasSuffix(path, "/logs"):
			dat, err := os.ReadFile("testdata/flog.log")
			require.NoError(t, err)
			_, err = w.Write(dat)
			require.NoError(t, err)
		default:
			w.Header().Set("Content-Type", "application/json")
			info := types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{},
				Mounts:            []types.MountPoint{},
				Config:            &container.Config{Tty: false},
				NetworkSettings:   &types.NetworkSettings{},
			}
			err := json.NewEncoder(w).Encode(info)
			require.NoError(t, err)
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(h))
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

	_, err = NewTarget(
		NewMetrics(prometheus.NewRegistry()),
		logger,
		entryHandler,
		ps,
		"flog",
		model.LabelSet{"job": "docker"},
		[]*relabel.Config{},
		client,
	)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(entryHandler.Received()) >= 5
	}, 5*time.Second, 100*time.Millisecond)

	received := entryHandler.Received()
	sort.Slice(received, func(i, j int) bool {
		return received[i].Timestamp.Before(received[j].Timestamp)
	})

	expectedLines := []string{
		"5.3.69.55 - - [09/Dec/2021:09:15:02 +0000] \"HEAD /brand/users/clicks-and-mortar/front-end HTTP/2.0\" 503 27087",
		"101.54.183.185 - - [09/Dec/2021:09:15:03 +0000] \"POST /next-generation HTTP/1.0\" 416 11468",
		"69.27.137.160 - runolfsdottir2670 [09/Dec/2021:09:15:03 +0000] \"HEAD /content/visionary/engineer/cultivate HTTP/1.1\" 302 2975",
		"28.104.242.74 - - [09/Dec/2021:09:15:03 +0000] \"PATCH /value-added/cultivate/systems HTTP/2.0\" 405 11843",
		"150.187.51.54 - satterfield1852 [09/Dec/2021:09:15:03 +0000] \"GET /incentivize/deliver/innovative/cross-platform HTTP/1.1\" 301 13032",
	}
	actualLines := make([]string, 0, 5)
	for _, entry := range received[:5] {
		actualLines = append(actualLines, entry.Line)
	}
	require.ElementsMatch(t, actualLines, expectedLines)
}
