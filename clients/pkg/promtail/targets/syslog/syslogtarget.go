package syslog

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var (
	defaultIdleTimeout      = 120 * time.Second
	defaultMaxMessageLength = 8192
	defaultProtocol         = protocolTCP
)

// SyslogTarget listens to syslog messages.
// nolint:revive
type SyslogTarget struct {
	metrics       *Metrics
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrapeconfig.SyslogTargetConfig
	relabelConfig []*relabel.Config

	transport Transport

	messages     chan message
	messagesDone chan struct{}
}

type message struct {
	labels    model.LabelSet
	message   string
	timestamp time.Time
}

// NewSyslogTarget configures a new SyslogTarget.
func NewSyslogTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	config *scrapeconfig.SyslogTargetConfig,
) (*SyslogTarget, error) {

	if config.SyslogFormat == "" {
		config.SyslogFormat = "rfc5424"
	}

	t := &SyslogTarget{
		metrics:       metrics,
		logger:        logger,
		handler:       handler,
		config:        config,
		relabelConfig: relabel,
		messagesDone:  make(chan struct{}),
	}

	switch t.transportProtocol() {
	case protocolTCP:
		t.transport = NewSyslogTCPTransport(
			config,
			t.handleMessage,
			t.handleMessageError,
			logger,
		)
	case protocolUDP:
		t.transport = NewSyslogUDPTransport(
			config,
			t.handleMessage,
			t.handleMessageError,
			logger,
		)
	default:
		return nil, fmt.Errorf("invalid transport protocol. expected 'tcp' or 'udp', got '%s'", t.transportProtocol())
	}

	t.messages = make(chan message)
	go t.messageSender(handler.Chan())

	err := t.transport.Run()
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (t *SyslogTarget) handleMessageError(err error) {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		level.Debug(t.logger).Log("msg", "connection timed out", "err", ne)
		return
	}
	level.Warn(t.logger).Log("msg", "error parsing syslog stream", "err", err)
	t.metrics.syslogParsingErrors.Inc()
}

func (t *SyslogTarget) handleMessageRFC5424(connLabels labels.Labels, msg syslog.Message) {
	rfc5424Msg := msg.(*rfc5424.SyslogMessage)

	if rfc5424Msg.Message == nil {
		t.metrics.syslogEmptyMessages.Inc()
		return
	}

	lb := labels.NewBuilder(connLabels)
	if v := rfc5424Msg.SeverityLevel(); v != nil {
		lb.Set("__syslog_message_severity", *v)
	}
	if v := rfc5424Msg.FacilityLevel(); v != nil {
		lb.Set("__syslog_message_facility", *v)
	}
	if v := rfc5424Msg.Hostname; v != nil {
		lb.Set("__syslog_message_hostname", *v)
	}
	if v := rfc5424Msg.Appname; v != nil {
		lb.Set("__syslog_message_app_name", *v)
	}
	if v := rfc5424Msg.ProcID; v != nil {
		lb.Set("__syslog_message_proc_id", *v)
	}
	if v := rfc5424Msg.MsgID; v != nil {
		lb.Set("__syslog_message_msg_id", *v)
	}

	if t.config.LabelStructuredData && rfc5424Msg.StructuredData != nil {
		for id, params := range *rfc5424Msg.StructuredData {
			id = strings.ReplaceAll(id, "@", "_")
			for name, value := range params {
				key := "__syslog_message_sd_" + id + "_" + name
				lb.Set(key, value)
			}
		}
	}

	processed, _ := relabel.Process(lb.Labels(), t.relabelConfig...)

	filtered := make(model.LabelSet)
	for _, lbl := range processed {
		if strings.HasPrefix(lbl.Name, "__") {
			continue
		}
		filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	var timestamp time.Time
	if t.config.UseIncomingTimestamp && rfc5424Msg.Timestamp != nil {
		timestamp = *rfc5424Msg.Timestamp
	} else {
		timestamp = time.Now()
	}

	m := *rfc5424Msg.Message
	if t.config.UseRFC5424Message {
		fullMsg, err := rfc5424Msg.String()
		if err != nil {
			level.Debug(t.logger).Log("msg", "failed to convert rfc5424 message to string; using message field instead", "err", err)
		} else {
			m = fullMsg
		}
	}
	t.messages <- message{filtered, m, timestamp}
}

func (t *SyslogTarget) handleMessageRFC3164(connLabels labels.Labels, msg syslog.Message) {
	rfc3164Msg := msg.(*rfc3164.SyslogMessage)

	if rfc3164Msg.Message == nil {
		t.metrics.syslogEmptyMessages.Inc()
		return
	}

	lb := labels.NewBuilder(connLabels)
	if v := rfc3164Msg.SeverityLevel(); v != nil {
		lb.Set("__syslog_message_severity", *v)
	}
	if v := rfc3164Msg.FacilityLevel(); v != nil {
		lb.Set("__syslog_message_facility", *v)
	}
	if v := rfc3164Msg.Hostname; v != nil {
		lb.Set("__syslog_message_hostname", *v)
	}
	if v := rfc3164Msg.Appname; v != nil {
		lb.Set("__syslog_message_app_name", *v)
	}
	if v := rfc3164Msg.ProcID; v != nil {
		lb.Set("__syslog_message_proc_id", *v)
	}
	if v := rfc3164Msg.MsgID; v != nil {
		lb.Set("__syslog_message_msg_id", *v)
	}

	processed, _ := relabel.Process(lb.Labels(), t.relabelConfig...)

	filtered := make(model.LabelSet)
	for _, lbl := range processed {
		if strings.HasPrefix(lbl.Name, "__") {
			continue
		}
		filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	var timestamp time.Time
	if t.config.UseIncomingTimestamp && rfc3164Msg.Timestamp != nil {
		timestamp = *rfc3164Msg.Timestamp
	} else {
		timestamp = time.Now()
	}

	m := *rfc3164Msg.Message

	t.messages <- message{filtered, m, timestamp}
}

func (t *SyslogTarget) handleMessage(connLabels labels.Labels, msg syslog.Message) {
	if t.config.IsRFC3164Message() {
		t.handleMessageRFC3164(connLabels, msg)
	} else {
		t.handleMessageRFC5424(connLabels, msg)
	}
}

func (t *SyslogTarget) messageSender(entries chan<- api.Entry) {
	for msg := range t.messages {
		entries <- api.Entry{
			Labels: msg.labels,
			Entry: logproto.Entry{
				Timestamp: msg.timestamp,
				Line:      msg.message,
			},
		}
		t.metrics.syslogEntries.Inc()
	}
	t.messagesDone <- struct{}{}
}

// Type returns SyslogTargetType.
func (t *SyslogTarget) Type() target.TargetType {
	return target.SyslogTargetType
}

// Ready indicates whether or not the syslog target is ready to be read from.
func (t *SyslogTarget) Ready() bool {
	return t.transport.Ready()
}

// DiscoveredLabels returns the set of labels discovered by the syslog target, which
// is always nil. Implements Target.
func (t *SyslogTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the SyslogTarget.
func (t *SyslogTarget) Labels() model.LabelSet {
	return t.config.Labels
}

// Details returns target-specific details.
func (t *SyslogTarget) Details() interface{} {
	return map[string]string{}
}

// Stop shuts down the SyslogTarget.
func (t *SyslogTarget) Stop() error {
	err := t.transport.Close()
	t.transport.Wait()
	close(t.messages)
	// wait for all pending messages to be processed and sent to handler
	<-t.messagesDone
	t.handler.Stop()
	return err
}

// ListenAddress returns the address SyslogTarget is listening on.
func (t *SyslogTarget) ListenAddress() net.Addr {
	return t.transport.Addr()
}

func (t *SyslogTarget) transportProtocol() string {
	if t.config.ListenProtocol != "" {
		return t.config.ListenProtocol
	}
	return defaultProtocol
}
