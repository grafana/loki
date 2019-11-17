package targets

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/promtail/scrape"
)

type ClientMessage struct {
	Labels    model.LabelSet
	Timestamp time.Time
	Message   string
}

type TestLabeledClient struct {
	log      log.Logger
	messages []ClientMessage
	sync.RWMutex
}

func (c *TestLabeledClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	level.Debug(c.log).Log("msg", "received log", "log", s)

	c.Lock()
	defer c.Unlock()
	c.messages = append(c.messages, ClientMessage{ls, t, s})
	return nil
}

func (c *TestLabeledClient) Messages() []ClientMessage {
	c.RLock()
	defer c.RUnlock()

	return c.messages
}

func TestSyslogTarget_NewlineSeparatedMessages(t *testing.T) {
	testSyslogTarget(t, false)
}

func TestSyslogTarget_OctetCounting(t *testing.T) {
	testSyslogTarget(t, true)
}

func testSyslogTarget(t *testing.T, octetCounting bool) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := &TestLabeledClient{log: logger}

	tgt, err := NewSyslogTarget(logger, client, relabelConfig(t), &scrape.SyslogTargetConfig{
		ListenAddress:       "127.0.0.1:0",
		LabelStructuredData: true,
		Labels: model.LabelSet{
			"test": "syslog_target",
		},
	})
	require.NoError(t, err)
	defer tgt.Stop()

	addr := tgt.ListenAddress().String()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer c.Close()

	messages := []string{
		`<165>1 2018-10-11T22:14:15.003Z host5 e - id1 [custom@32473 exkey="1"] An application event log entry...`,
		`<165>1 2018-10-11T22:14:15.005Z host5 e - id2 [custom@32473 exkey="2"] An application event log entry...`,
		`<165>1 2018-10-11T22:14:15.007Z host5 e - id3 [custom@32473 exkey="3"] An application event log entry...`,
	}

	err = writeMessagesToStream(c, messages, octetCounting)
	require.NoError(t, err)

	require.Eventuallyf(t, func() bool {
		return len(client.Messages()) == len(messages)
	}, time.Second, time.Millisecond, "Expected to receive %d messages, got %d.", len(messages), len(client.Messages()))

	require.Equal(t, model.LabelSet{
		"test": "syslog_target",

		"severity": "notice",
		"facility": "local4",
		"hostname": "host5",
		"app_name": "e",
		"msg_id":   "id1",

		"sd_custom_exkey": "1",
	}, client.Messages()[0].Labels)
	require.Equal(t, "An application event log entry...", client.Messages()[0].Message)

	require.NotZero(t, client.Messages()[0].Timestamp)
}

func relabelConfig(t *testing.T) []*relabel.Config {
	relabelCfg := `
- source_labels: ['__syslog_message_severity']
  target_label: 'severity'
- source_labels: ['__syslog_message_facility']
  target_label: 'facility'
- source_labels: ['__syslog_message_hostname']
  target_label: 'hostname'
- source_labels: ['__syslog_message_app_name']
  target_label: 'app_name'
- source_labels: ['__syslog_message_proc_id']
  target_label: 'proc_id'
- source_labels: ['__syslog_message_msg_id']
  target_label: 'msg_id'
- source_labels: ['__syslog_message_sd_custom_32473_exkey']
  target_label: 'sd_custom_exkey'
`

	var relabels []*relabel.Config
	err := yaml.Unmarshal([]byte(relabelCfg), &relabels)
	require.NoError(t, err)

	return relabels
}

func writeMessagesToStream(w io.Writer, messages []string, octetCounting bool) error {
	var formatter func(string) string

	if octetCounting {
		formatter = func(s string) string {
			return fmt.Sprintf("%d %s", len(s), s)
		}
	} else {
		formatter = func(s string) string {
			return s + "\n"
		}
	}

	for _, msg := range messages {
		_, err := fmt.Fprint(w, formatter(msg))
		if err != nil {
			return err
		}
	}

	return nil
}

func TestSyslogTarget_InvalidData(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := &TestLabeledClient{log: logger}

	tgt, err := NewSyslogTarget(logger, client, relabelConfig(t), &scrape.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
	})
	require.NoError(t, err)
	defer tgt.Stop()

	addr := tgt.ListenAddress().String()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer c.Close()

	_, err = fmt.Fprint(c, "xxx")

	// syslog target should immediately close the connection if sent invalid data
	c.SetDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	_, err = c.Read(buf)
	require.EqualError(t, err, "EOF")
}

func TestSyslogTarget_IdleTimeout(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := &TestLabeledClient{log: logger}

	tgt, err := NewSyslogTarget(logger, client, relabelConfig(t), &scrape.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
		IdleTimeout:   time.Millisecond,
	})
	require.NoError(t, err)
	defer tgt.Stop()

	addr := tgt.ListenAddress().String()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer c.Close()

	// connection should be closed before the higher timeout
	// from SetDeadline fires
	c.SetDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	_, err = c.Read(buf)
	require.EqualError(t, err, "EOF")
}
