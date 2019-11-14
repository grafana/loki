package targets

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

type ClientMessage struct {
	Labels    model.LabelSet
	Timestamp time.Time
	Message   string
}

type TestLabeledClient struct {
	log      log.Logger
	messages []ClientMessage
	sync.Mutex
}

func (c *TestLabeledClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	level.Debug(c.log).Log("msg", "received log", "log", s)

	c.Lock()
	defer c.Unlock()
	c.messages = append(c.messages, ClientMessage{ls, t, s})
	return nil
}

func TestHTTPTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := &TestLabeledClient{log: logger}

	tgt, err := NewHTTPTarget(logger, client, nil, &scrape.HTTPTargetConfig{})
	require.NoError(t, err)

	req, err := http.NewRequest("GET", "label1/value1/label2/value2/", strings.NewReader("hello, world"))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	tgt.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, len(client.messages), 1)
	require.Equal(t, model.LabelSet{
		"label1": "value1",
		"label2": "value2",
	}, client.messages[0].Labels)
	require.Equal(t, "hello, world", client.messages[0].Message)
	require.NotZero(t, client.messages[0].Timestamp)
}

func TestHTTPTarget_Timestamp(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := &TestLabeledClient{log: logger}

	tgt, err := NewHTTPTarget(logger, client, nil, &scrape.HTTPTargetConfig{})
	require.NoError(t, err)

	// Get a timestamp trimmed to precision defined by RFC3339Nano
	ts := time.Now()
	ts, _ = time.Parse(time.RFC3339Nano, ts.Format(time.RFC3339Nano))

	path := fmt.Sprintf("label1/value1/label2/value2?ts=%s", ts.Format(time.RFC3339Nano))
	req, err := http.NewRequest("GET", path, strings.NewReader("hello, world"))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	tgt.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, len(client.messages), 1)
	require.Equal(t, ClientMessage{
		Labels:    model.LabelSet{"label1": "value1", "label2": "value2"},
		Timestamp: ts,
		Message:   "hello, world",
	}, client.messages[0])
}
