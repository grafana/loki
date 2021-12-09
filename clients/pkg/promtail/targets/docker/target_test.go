package docker

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
)

func Test_CloudflareTarget(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		dat, err := os.ReadFile("testdata/flog.log")
		require.NoError(t, err)
		_, err = w.Write(dat)
		require.NoError(t, err)
	}

	ts := httptest.NewServer(http.HandlerFunc(h))
	defer ts.Close()

	var (
		w      = log.NewSyncWriter(os.Stderr)
		logger = log.NewLogfmtLogger(w)
		cfg    = &scrapeconfig.DockerConfig{
			Host:          ts.URL,
			Labels:        model.LabelSet{"job": "docker"},
			ContainerName: "flog",
		}
		client = fake.New(func() {})
	)

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	require.NoError(t, err)

	ta, err := NewTarget(NewMetrics(prometheus.NewRegistry()), logger, client, ps, cfg)
	require.NoError(t, err)
	require.True(t, ta.Ready())

	require.Eventually(t, func() bool {
		return len(client.Received()) >= 5
	}, 5*time.Second, 100*time.Millisecond)

	received := client.Received()
	sort.Slice(received, func(i, j int) bool {
		return received[i].Timestamp.Before(received[j].Timestamp)
	})

	expectedLines := []string{
		"5.3.69.55 - - [09/Dec/2021:09:15:02 +0000] \"HEAD /brand/users/clicks-and-mortar/front-end HTTP/2.0\" 503 27087\n",
		"101.54.183.185 - - [09/Dec/2021:09:15:03 +0000] \"POST /next-generation HTTP/1.0\" 416 11468\n",
		"69.27.137.160 - runolfsdottir2670 [09/Dec/2021:09:15:03 +0000] \"HEAD /content/visionary/engineer/cultivate HTTP/1.1\" 302 2975\n",
		"28.104.242.74 - - [09/Dec/2021:09:15:03 +0000] \"PATCH /value-added/cultivate/systems HTTP/2.0\" 405 11843\n",
		"150.187.51.54 - satterfield1852 [09/Dec/2021:09:15:03 +0000] \"GET /incentivize/deliver/innovative/cross-platform HTTP/1.1\" 301 13032\n",
	}
	actualLines := make([]string, 0, 5)
	for _, entry := range received[:5] {
		actualLines = append(actualLines, entry.Line)
	}
	require.ElementsMatch(t, actualLines, expectedLines)
}
