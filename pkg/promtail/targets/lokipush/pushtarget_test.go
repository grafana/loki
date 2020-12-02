package lokipush

import (
	"flag"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/client/fake"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
)

func TestPushTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	//Create PushTarget
	eh := fake.New(func() {})
	defer eh.Stop()

	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	require.NoError(t, err)

	// Adjust some of the defaults
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	defaults.HTTPListenAddress = "127.0.0.1"
	defaults.HTTPListenPort = port
	defaults.GRPCListenAddress = "127.0.0.1"
	defaults.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	config := &scrapeconfig.PushTargetConfig{
		Server: defaults,
		Labels: model.LabelSet{
			"pushserver": "pushserver1",
			"dropme":     "label",
		},
		KeepTimestamp: true,
	}

	rlbl := []*relabel.Config{
		{
			Action: relabel.LabelDrop,
			Regex:  relabel.MustNewRegexp("dropme"),
		},
	}

	pt, err := NewPushTarget(logger, eh, rlbl, "job1", config)
	require.NoError(t, err)

	// Build a client to send logs
	serverURL := flagext.URLValue{}
	err = serverURL.Set("http://127.0.0.1:" + strconv.Itoa(port) + "/loki/api/v1/push")
	require.NoError(t, err)

	ccfg := client.Config{
		URL:       serverURL,
		Timeout:   1 * time.Second,
		BatchWait: 1 * time.Second,
		BatchSize: 100 * 1024,
	}
	pc, err := client.New(ccfg, logger)
	require.NoError(t, err)
	defer pc.Stop()

	// Send some logs
	labels := model.LabelSet{
		"stream":             "stream1",
		"__anotherdroplabel": "dropme",
	}
	for i := 0; i < 100; i++ {
		pc.Chan() <- api.Entry{
			Labels: labels,
			Entry: logproto.Entry{
				Timestamp: time.Unix(int64(i), 0),
				Line:      "line" + strconv.Itoa(i),
			},
		}
	}

	// Wait for them to appear in the test handler
	countdown := 10000
	for len(eh.Received()) != 100 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	// Make sure we didn't timeout
	require.Equal(t, 100, len(eh.Received()))

	// Verify labels
	expectedLabels := model.LabelSet{
		"pushserver": "pushserver1",
		"stream":     "stream1",
	}
	// Spot check the first value in the result to make sure relabel rules were applied properly
	require.Equal(t, expectedLabels, eh.Received()[0].Labels)

	// With keep timestamp enabled, verify timestamp
	require.Equal(t, time.Unix(99, 0).Unix(), eh.Received()[99].Timestamp.Unix())

	_ = pt.Stop()

}
