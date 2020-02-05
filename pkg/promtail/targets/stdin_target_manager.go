package targets

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/prometheus/client_golang/prometheus"
)

// bufferSize is the size of the buffered reader
const bufferSize = 8096

func isPipe() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		level.Warn(util.Logger).Log("err", err)
		return false
	}
	m := info.Mode()
	if m&os.ModeCharDevice != 0 || info.Size() <= 0 {
		return false
	}
	return true
}

type Shutdownable interface {
	Shutdown()
}

type stdinTargetManager struct {
	*readerTarget
	app Shutdownable
}

func newStdinTargetManager(app Shutdownable, client api.EntryHandler, configs []scrape.Config) (*stdinTargetManager, error) {
	cfg := scrape.Config{
		JobName: "stdin",
	}
	// if we receive configs we use the first one.
	if len(configs) > 0 {
		if len(configs) > 1 {
			level.Warn(util.Logger).Log("msg", "too many scrape configs", "skipped", len(configs)-1)
		}
		cfg = configs[0]
	}
	reader, err := newReaderTarget(os.Stdin, client, cfg)
	if err != nil {
		return nil, err
	}
	stdinManager := &stdinTargetManager{
		readerTarget: reader,
		app:          app,
	}
	return stdinManager, nil
}

func (t *stdinTargetManager) Ready() bool {
	select {
	case <-t.ctx.Done():
		return false
	default:
		return true
	}
}
func (t *stdinTargetManager) Stop()                              { t.cancel() }
func (t *stdinTargetManager) ActiveTargets() map[string][]Target { return nil }
func (t *stdinTargetManager) AllTargets() map[string][]Target    { return nil }

type readerTarget struct {
	in  *bufio.Reader
	out api.EntryHandler

	logger log.Logger

	cancel context.CancelFunc
	ctx    context.Context
}

func newReaderTarget(in io.Reader, client api.EntryHandler, cfg scrape.Config) (*readerTarget, error) {
	if cfg.HasServiceDiscoveryConfig() {
		return nil, errors.New("reader target does not support service discovery")
	}
	pipeline, err := stages.NewPipeline(log.With(util.Logger, "component", "pipeline"), cfg.PipelineStages, &cfg.JobName, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &readerTarget{
		in:     bufio.NewReaderSize(in, bufferSize),
		out:    pipeline.Wrap(client),
		cancel: cancel,
		ctx:    ctx,
		logger: log.With(util.Logger, "component", "reader"),
	}
	go t.read()

	return t, nil
}

func (t *readerTarget) read() {
	defer t.cancel()

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
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
			if err := t.out.Handle(nil, time.Now(), line); err != nil {
				level.Error(t.logger).Log("msg", "error sending line", "err", err)
			}
			if err == io.EOF {
				return
			}
		}
	}
}
