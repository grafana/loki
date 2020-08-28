package stdin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

// bufferSize is the size of the buffered reader
const bufferSize = 8096

// file is an interface allowing us to abstract a file.
type file interface {
	Stat() (os.FileInfo, error)
	io.Reader
}

var (
	// stdIn is os.Stdin but can be replaced for testing purpose.
	stdIn       file = os.Stdin
	hostName, _      = os.Hostname()
	// defaultStdInCfg is the default config for stdin target if none provided.
	defaultStdInCfg = scrapeconfig.Config{
		JobName: "stdin",
		ServiceDiscoveryConfig: config.ServiceDiscoveryConfig{
			StaticConfigs: []*targetgroup.Group{
				{Labels: model.LabelSet{"job": "stdin"}},
				{Labels: model.LabelSet{"hostname": model.LabelValue(hostName)}},
			},
		},
	}
)

type Shutdownable interface {
	Shutdown()
}

type stdinTargetManager struct {
	*readerTarget
	app Shutdownable
}

func NewStdinTargetManager(log log.Logger, app Shutdownable, client api.EntryHandler, configs []scrapeconfig.Config) (*stdinTargetManager, error) {
	reader, err := newReaderTarget(log, stdIn, client, getStdinConfig(log, configs))
	if err != nil {
		return nil, err
	}
	stdinManager := &stdinTargetManager{
		readerTarget: reader,
		app:          app,
	}
	// when we're done flushing our stdin we can shutdown the app.
	go func() {
		<-reader.ctx.Done()
		app.Shutdown()
	}()
	return stdinManager, nil
}

func getStdinConfig(log log.Logger, configs []scrapeconfig.Config) scrapeconfig.Config {
	cfg := defaultStdInCfg
	// if we receive configs we use the first one.
	if len(configs) > 0 {
		if len(configs) > 1 {
			level.Warn(log).Log("msg", fmt.Sprintf("too many scrape configs, skipping %d configs.", len(configs)-1))
		}
		cfg = configs[0]
	}
	return cfg
}

func (t *stdinTargetManager) Ready() bool {
	return t.ctx.Err() == nil
}
func (t *stdinTargetManager) Stop()                                     { t.cancel() }
func (t *stdinTargetManager) ActiveTargets() map[string][]target.Target { return nil }
func (t *stdinTargetManager) AllTargets() map[string][]target.Target    { return nil }

type readerTarget struct {
	in     *bufio.Reader
	out    api.EntryHandler
	lbs    model.LabelSet
	logger log.Logger

	cancel context.CancelFunc
	ctx    context.Context
}

func newReaderTarget(logger log.Logger, in io.Reader, client api.EntryHandler, cfg scrapeconfig.Config) (*readerTarget, error) {
	pipeline, err := stages.NewPipeline(log.With(logger, "component", "pipeline"), cfg.PipelineStages, &cfg.JobName, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}
	lbs := model.LabelSet{}
	for _, static := range cfg.ServiceDiscoveryConfig.StaticConfigs {
		if static != nil && static.Labels != nil {
			lbs = lbs.Merge(static.Labels)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &readerTarget{
		in:     bufio.NewReaderSize(in, bufferSize),
		out:    pipeline.Wrap(client),
		cancel: cancel,
		ctx:    ctx,
		lbs:    lbs,
		logger: log.With(logger, "component", "reader"),
	}
	go t.read()

	return t, nil
}

func (t *readerTarget) read() {
	defer t.cancel()

	for {
		if t.ctx.Err() != nil {
			return
		}
		line, err := t.in.ReadString('\n')
		if err != nil && err != io.EOF {
			level.Warn(t.logger).Log("msg", "error reading buffer", "err", err)
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err == io.EOF {
				return
			}
			continue
		}
		if err := t.out.Handle(t.lbs.Clone(), time.Now(), line); err != nil {
			level.Error(t.logger).Log("msg", "error sending line", "err", err)
		}
		if err == io.EOF {
			return
		}
	}
}
