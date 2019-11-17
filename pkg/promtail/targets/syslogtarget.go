package targets

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/influxdata/go-syslog"
	"github.com/mwitkow/go-conntrack"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/grafana/loki/pkg/promtail/targets/syslogparser"
)

var (
	syslogEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "syslog_target_entries_total",
		Help:      "Total number of successful entries sent to the syslog target",
	})
	syslogParsingErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "syslog_target_parsing_errors_total",
		Help:      "Total number of parsing errors while receiving syslog messages",
	})

	defaultIdleTimeout = 120 * time.Second
)

// SyslogTarget listens to syslog messages.
type SyslogTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrape.SyslogTargetConfig
	relabelConfig []*relabel.Config

	listener net.Listener
	messages chan message

	shutdown          chan struct{}
	connectionsClosed *sync.WaitGroup
}

type message struct {
	labels  model.LabelSet
	message string
}

// NewSyslogTarget configures a new SyslogTarget.
func NewSyslogTarget(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	config *scrape.SyslogTargetConfig,
) (*SyslogTarget, error) {

	t := &SyslogTarget{
		logger:        logger,
		handler:       handler,
		config:        config,
		relabelConfig: relabel,

		shutdown:          make(chan struct{}),
		connectionsClosed: new(sync.WaitGroup),
	}

	t.messages = make(chan message)
	go t.messageSender()

	err := t.run()
	return t, err
}

func (t *SyslogTarget) run() error {
	l, err := net.Listen("tcp", t.config.ListenAddress)
	l = conntrack.NewListener(l, conntrack.TrackWithName("syslog_target/"+t.config.ListenAddress))
	if err != nil {
		return fmt.Errorf("error setting up syslog target %w", err)
	}
	t.listener = l
	level.Info(t.logger).Log("msg", "syslog listening on address", "address", t.ListenAddress().String())

	t.connectionsClosed.Add(1)
	go t.acceptConnections()

	return nil
}

func (t *SyslogTarget) acceptConnections() {
	defer t.connectionsClosed.Done()

	l := log.With(t.logger, "address", t.listener.Addr().String())
	var backoff time.Duration

	for {
		c, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.shutdown:
				level.Info(l).Log("msg", "syslog server shutting down")
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if backoff == 0 {
					backoff = 5 * time.Millisecond
				} else {
					backoff *= 2
				}
				if max := 1 * time.Second; backoff > max {
					backoff = max
				}
				level.Warn(l).Log("msg", "failed to accept syslog connection", "err", err, "retry_in", backoff)
				time.Sleep(backoff)
				continue
			}

			level.Error(l).Log("msg", "failed to accept syslog connection. quiting", "err", err)
			return
		}

		t.connectionsClosed.Add(1)
		go t.handleConnection(c)
	}
}

func (t *SyslogTarget) handleConnection(cn net.Conn) {
	defer t.connectionsClosed.Done()

	c := &idleTimeoutConn{cn, t.idleTimeout()}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-t.shutdown:
		}
		c.Close()
	}()

	connLabels := t.connectionLabels(c)

	for msg := range syslogparser.ParseStream(c) {
		if err := msg.Error; err != nil {
			t.handleMessageError(err)
			continue
		}
		t.handleMessage(connLabels.Copy(), msg.Message)
	}
}

func (t *SyslogTarget) handleMessageError(err error) {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		level.Debug(t.logger).Log("msg", "connection timed out", "err", ne)
		return
	}
	level.Debug(t.logger).Log("msg", "error parsing syslog stream", "err", err)
	syslogParsingErrors.Inc()
}

func (t *SyslogTarget) handleMessage(connLabels labels.Labels, msg syslog.Message) {
	if msg.Message() == nil {
		return
	}

	lb := labels.NewBuilder(connLabels)
	if v := msg.SeverityLevel(); v != nil {
		lb.Set("__syslog_message_severity", *v)
	}
	if v := msg.FacilityLevel(); v != nil {
		lb.Set("__syslog_message_facility", *v)
	}
	if v := msg.Hostname(); v != nil {
		lb.Set("__syslog_message_hostname", *v)
	}
	if v := msg.Appname(); v != nil {
		lb.Set("__syslog_message_app_name", *v)
	}
	if v := msg.ProcID(); v != nil {
		lb.Set("__syslog_message_proc_id", *v)
	}
	if v := msg.MsgID(); v != nil {
		lb.Set("__syslog_message_msg_id", *v)
	}

	if t.config.LabelStructuredData && msg.StructuredData() != nil {
		for id, params := range *msg.StructuredData() {
			id = strings.Replace(id, "@", "_", -1)
			for name, value := range params {
				key := "__syslog_message_sd_" + id + "_" + name
				lb.Set(key, value)
			}
		}
	}

	processed := relabel.Process(lb.Labels(), t.relabelConfig...)

	filtered := make(model.LabelSet)
	for _, lbl := range processed {
		if len(lbl.Name) >= 2 && lbl.Name[0:2] == "__" {
			continue
		}
		filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	t.messages <- message{filtered, *msg.Message()}
}

func (t *SyslogTarget) messageSender() {
	for msg := range t.messages {
		t.handler.Handle(msg.labels, time.Now(), msg.message)
		syslogEntries.Inc()
	}
}

func (t *SyslogTarget) connectionLabels(c net.Conn) labels.Labels {
	lb := labels.NewBuilder(nil)
	for k, v := range t.config.Labels {
		lb.Set(string(k), string(v))
	}

	ip := ipFromConn(c).String()
	lb.Set("__syslog_connection_ip_address", ip)
	lb.Set("__syslog_connection_hostname", lookupAddr(ip))

	return lb.Labels()
}

func ipFromConn(c net.Conn) net.IP {
	switch addr := c.RemoteAddr().(type) {
	case *net.TCPAddr:
		return addr.IP
	}

	return nil
}

func lookupAddr(addr string) string {
	names, _ := net.LookupAddr(addr)
	return strings.Join(names, ",")
}

// Type returns SyslogTargetType.
func (t *SyslogTarget) Type() TargetType {
	return SyslogTargetType
}

// Ready indicates whether or not the syslog target is ready to be read from.
func (t *SyslogTarget) Ready() bool {
	return true
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
	close(t.shutdown)
	err := t.listener.Close()
	t.connectionsClosed.Wait()
	close(t.messages)
	return err
}

// ListenAddress returns the address SyslogTarget is listening on.
func (t *SyslogTarget) ListenAddress() net.Addr {
	return t.listener.Addr()
}

func (t *SyslogTarget) idleTimeout() time.Duration {
	if tm := t.config.IdleTimeout; tm != 0 {
		return tm
	}
	return defaultIdleTimeout
}

type idleTimeoutConn struct {
	net.Conn
	idleTimeout time.Duration
}

func (c *idleTimeoutConn) Write(p []byte) (int, error) {
	c.setDeadline()
	return c.Conn.Write(p)
}

func (c *idleTimeoutConn) Read(b []byte) (int, error) {
	c.setDeadline()
	return c.Conn.Read(b)
}

func (c *idleTimeoutConn) setDeadline() {
	c.Conn.SetDeadline(time.Now().Add(c.idleTimeout))
}
