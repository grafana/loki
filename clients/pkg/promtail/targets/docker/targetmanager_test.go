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
	"github.com/docker/docker/api/types/network"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/moby"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func Test_TargetManager(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		switch path := r.URL.Path; {
		case path == "/_ping":
			_, err := w.Write([]byte("OK"))
			require.NoError(t, err)
		case strings.HasSuffix(path, "/containers/json"):
			// Serve container list
			w.Header().Set("Content-Type", "application/json")
			containerResponse := []types.Container{{
				ID:    "1234",
				Names: []string{"flog"},
				NetworkSettings: &types.SummaryNetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"foo": {
							NetworkID: "my_network",
							IPAddress: "127.0.0.1",
						},
					},
				},
			}}
			err := json.NewEncoder(w).Encode(containerResponse)
			require.NoError(t, err)
		case strings.HasSuffix(path, "/networks"):
			// Serve networks
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode([]network.Inspect{})
			require.NoError(t, err)
		case strings.HasSuffix(path, "json"):
			w.Header().Set("Content-Type", "application/json")
			info := types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{},
				Mounts:            []types.MountPoint{},
				Config:            &container.Config{Tty: false},
				NetworkSettings:   &types.NetworkSettings{},
			}
			err := json.NewEncoder(w).Encode(info)
			require.NoError(t, err)
		default:
			// Serve container logs
			dat, err := os.ReadFile("testdata/flog.log")
			require.NoError(t, err)
			_, err = w.Write(dat)
			require.NoError(t, err)
		}
	}
	dockerDaemonMock := httptest.NewServer(http.HandlerFunc(h))
	defer dockerDaemonMock.Close()

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	entryHandler := fake.New(func() {})
	cfgs := []scrapeconfig.Config{{
		DockerSDConfigs: []*moby.DockerSDConfig{{
			Host:            dockerDaemonMock.URL,
			RefreshInterval: model.Duration(100 * time.Millisecond),
		}},
	}}

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	require.NoError(t, err)

	ta, err := NewTargetManager(
		NewMetrics(prometheus.NewRegistry()),
		logger,
		ps,
		entryHandler,
		cfgs,
		0,
	)
	require.NoError(t, err)
	require.True(t, ta.Ready())

	require.Eventually(t, func() bool {
		return len(entryHandler.Received()) >= 6
	}, 20*time.Second, 100*time.Millisecond)

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
	require.Equal(t, 99969, len(received[5].Line))
}
