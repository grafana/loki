package kubernetes

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/go-kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	restFake "k8s.io/client-go/rest/fake"
)

func Test_KubernetesTarget(t *testing.T) {
	client := kubernetes.New(&restFake.RESTClient{
		Client: restFake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/namespaces/namespace1/pods/pod1/log":
				dat, err := os.ReadFile("testdata/flog.log")
				require.NoError(t, err)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(dat)),
				}, nil
			default:
				require.Failf(t, "unexpected path: %s", req.URL.Path)
				return nil, nil
			}
		}),
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		GroupVersion:         corev1.SchemeGroupVersion.WithKind("Pod").GroupVersion(),
	})

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	entryHandler := fake.New(func() {})

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
		"namespace1",
		"pod1",
		"container1",
		model.LabelSet{"job": "k8s"},
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
		"0: Sun Apr 30 12:04:38 UTC 2023",
		"1: Sun Apr 30 12:04:39 UTC 2023",
		"2: Sun Apr 30 12:04:40 UTC 2023",
		"3: Sun Apr 30 12:04:41 UTC 2023",
		"4: Sun Apr 30 12:04:42 UTC 2023",
	}
	actualLines := make([]string, 0, 5)
	for _, entry := range received[:5] {
		actualLines = append(actualLines, entry.Line)
	}
	require.ElementsMatch(t, actualLines, expectedLines)
}
