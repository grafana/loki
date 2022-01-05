package docker

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	docker_types "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/pkg/logproto"
)

type Target struct {
	logger        log.Logger
	handler       api.EntryHandler
	since         int64
	positions     positions.Positions
	containerName string
	labels        model.LabelSet
	metrics       *Metrics

	ctx     context.Context
	cancel  context.CancelFunc
	client  client.APIClient
	wg      sync.WaitGroup
	running *atomic.Bool
	err     error
}

func NewTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	position positions.Positions,
	containerName string,
	labels model.LabelSet,
	client client.APIClient,
) (*Target, error) {

	pos, err := position.Get(positions.CursorKey(containerName))
	if err != nil {
		return nil, err
	}
	var since int64 = 0
	if pos != 0 {
		since = pos
	}

	ctx, cancel := context.WithCancel(context.Background())
	t := &Target{
		logger:        logger,
		handler:       handler,
		since:         since,
		positions:     position,
		containerName: containerName,
		labels:        labels,
		metrics:       metrics,

		ctx:     ctx,
		cancel:  cancel,
		client:  client,
		running: atomic.NewBool(false),
	}
	t.start()
	return t, nil
}

func (t *Target) start() {
	t.running.Store(true)

	opts := docker_types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Since:      strconv.FormatInt(t.since, 10),
	}

	logs, err := t.client.ContainerLogs(t.ctx, t.containerName, opts)
	if err != nil {
		level.Error(t.logger).Log("msg", "could not fetch logs for container", "container", t.containerName, "err", err)
		t.err = err
		return
	}

	out := make(chan frame)
	dstout := stdoutWriter{out: out}
	dsterr := stderrWriter{out: out}

	// Start transfering
	go func() {
		t.wg.Add(1)
		defer func() {
			t.wg.Done()
			close(out)
		}()

		written, err := stdcopy.StdCopy(dstout, dsterr, logs)
		if err != nil {
			level.Error(t.logger).Log("msg", "could not transfer logs", "written", written, "err", err)
		} else {
			level.Info(t.logger).Log("msg", "finished transfering logs", "written", written)
		}
	}()

	// Start processing
	go func() {
		t.wg.Add(1)
		defer func() {
			t.wg.Done()
			t.running.Store(false)
			logs.Close()
		}()

		for t.ctx.Err() == nil {
			select {
			case f := <-out:
				if len(f.line) == 0 {
					continue
				}

				ts, line, err := extractTs(f.line)
				if err != nil {
					level.Error(t.logger).Log("msg", "could not extract timestamp", "err", err)
					t.metrics.dockerErrors.Inc()
				}
				level.Debug(t.logger).Log("msg", "sending log line", "line", line)

				t.handler.Chan() <- api.Entry{
					Labels: t.labels.Clone(),
					Entry: logproto.Entry{
						Timestamp: ts,
						Line:      line,
					},
				}
				t.metrics.dockerEntries.Inc()
				t.positions.Put(positions.CursorKey(t.containerName), ts.Unix())
			case <-t.ctx.Done():
				{
				}
			}
		}
	}()
}

func extractTs(line string) (time.Time, string, error) {
	pair := strings.SplitN(line, " ", 2)
	if len(pair) != 2 {
		return time.Now(), line, fmt.Errorf("Could not find timestamp in '%s'", line)
	}
	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", pair[0])
	if err != nil {
		return time.Now(), line, fmt.Errorf("Could not parse timestamp from '%s': %w", pair[0], err)
	}
	return ts, pair[1], nil
}

func (t *Target) Stop() {
	t.cancel()
	t.wg.Wait()
	t.handler.Stop()
}

func (t *Target) Type() target.TargetType {
	return target.DockerTargetType
}

func (t *Target) Ready() bool {
	return t.running.Load()
}

func (t *Target) DiscoveredLabels() model.LabelSet {
	return nil // TODO
}

func (t *Target) Labels() model.LabelSet {
	return t.labels
}

// Details returns target-specific details.
func (t *Target) Details() interface{} {
	return map[string]string{}
}

type frame struct {
	stream stdcopy.StdType
	line   string
}

type stdoutWriter struct {
	out chan frame
}

func (w stdoutWriter) Write(p []byte) (n int, err error) {
	f := frame{stream: stdcopy.Stdout, line: string(p)}
	w.out <- f
	return len(f.line), nil
}

type stderrWriter struct {
	out chan frame
}

func (w stderrWriter) Write(p []byte) (n int, err error) {
	f := frame{stream: stdcopy.Stderr, line: string(p)}
	w.out <- f
	return len(f.line), nil
}
