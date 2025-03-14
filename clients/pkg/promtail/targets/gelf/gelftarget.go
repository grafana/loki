package gelf

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/go-gelf/v2/gelf"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// SeverityLevels maps severity levels to severity string levels.
var SeverityLevels = map[int32]string{
	0: "emergency",
	1: "alert",
	2: "critical",
	3: "error",
	4: "warning",
	5: "notice",
	6: "informational",
	7: "debug",
}

// Target listens to gelf messages on udp.
type Target struct {
	metrics       *Metrics
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrapeconfig.GelfTargetConfig
	relabelConfig []*relabel.Config
	gelfReader    *gelf.Reader
	encodeBuff    *bytes.Buffer
	wg            sync.WaitGroup

	ctx       context.Context
	ctxCancel context.CancelFunc
}

// NewTarget configures a new Gelf Target.
func NewTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	config *scrapeconfig.GelfTargetConfig,
) (*Target, error) {

	if config.ListenAddress == "" {
		config.ListenAddress = ":12201"
	}

	gelfReader, err := gelf.NewReader(config.ListenAddress)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())

	t := &Target{
		metrics:       metrics,
		logger:        logger,
		handler:       handler,
		config:        config,
		relabelConfig: relabel,
		gelfReader:    gelfReader,
		encodeBuff:    bytes.NewBuffer(make([]byte, 0, 1024)),

		ctx:       ctx,
		ctxCancel: cancel,
	}

	t.run()
	return t, err
}

func (t *Target) run() {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		level.Info(t.logger).Log("msg", "listening for GELF UDP messages", "listen_address", t.config.ListenAddress)
		for {
			select {
			case <-t.ctx.Done():
				level.Info(t.logger).Log("msg", "GELF UDP listener shutdown", "listen_address", t.config.ListenAddress)
				return
			default:
				msg, err := t.gelfReader.ReadMessage()
				if err != nil {
					level.Error(t.logger).Log("msg", "error while reading gelf message", "listen_address", t.config.ListenAddress, "err", err)
					t.metrics.gelfErrors.Inc()
					continue
				}
				if msg != nil {
					t.metrics.gelfEntries.Inc()
					t.handleMessage(msg)
				}
			}
		}
	}()
}

func (t *Target) handleMessage(msg *gelf.Message) {
	lb := labels.NewBuilder(nil)

	// Add all labels from the config.
	for k, v := range t.config.Labels {
		lb.Set(string(k), string(v))
	}
	lb.Set("__gelf_message_level", SeverityLevels[msg.Level])
	lb.Set("__gelf_message_host", msg.Host)
	lb.Set("__gelf_message_version", msg.Version)
	lb.Set("__gelf_message_facility", msg.Facility)

	processed, _ := relabel.Process(lb.Labels(), t.relabelConfig...)

	filtered := make(model.LabelSet)
	for _, lbl := range processed {
		if strings.HasPrefix(lbl.Name, "__") {
			continue
		}
		filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	var timestamp time.Time
	if t.config.UseIncomingTimestamp && msg.TimeUnix != 0 {
		// TimeUnix is the timestamp of the message, in seconds since the UNIX epoch with decimals for fractional seconds.
		timestamp = secondsToUnixTimestamp(msg.TimeUnix)
	} else {
		timestamp = time.Now()
	}
	t.encodeBuff.Reset()
	err := msg.MarshalJSONBuf(t.encodeBuff)
	if err != nil {
		level.Error(t.logger).Log("msg", "error while marshalling gelf message", "listen_address", t.config.ListenAddress, "err", err)
		t.metrics.gelfErrors.Inc()
		return
	}
	t.handler.Chan() <- api.Entry{
		Labels: filtered,
		Entry: logproto.Entry{
			Timestamp: timestamp,
			Line:      t.encodeBuff.String(),
		},
	}
}

func secondsToUnixTimestamp(seconds float64) time.Time {
	return time.Unix(0, int64(seconds*float64(time.Second)))
}

// Type returns GelfTargetType.
func (t *Target) Type() target.TargetType {
	return target.GelfTargetType
}

// Ready indicates whether or not the gelf target is ready to be read from.
func (t *Target) Ready() bool {
	return true
}

// DiscoveredLabels returns the set of labels discovered by the gelf target, which
// is always nil. Implements Target.
func (t *Target) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the GelfTarget.
func (t *Target) Labels() model.LabelSet {
	return t.config.Labels
}

// Details returns target-specific details.
func (t *Target) Details() interface{} {
	return map[string]string{}
}

// Stop shuts down the GelfTarget.
func (t *Target) Stop() {
	level.Info(t.logger).Log("msg", "Shutting down GELF UDP listener", "listen_address", t.config.ListenAddress)
	t.ctxCancel()
	if err := t.gelfReader.Close(); err != nil {
		level.Error(t.logger).Log("msg", "error while closing gelf reader", "err", err)
	}
	t.wg.Wait()
	t.handler.Stop()
}
