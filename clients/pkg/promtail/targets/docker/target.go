package docker

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/framedstdcopy"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type Target struct {
	logger        log.Logger
	handler       api.EntryHandler
	since         int64
	positions     positions.Positions
	containerName string
	labels        model.LabelSet
	relabelConfig []*relabel.Config
	metrics       *Metrics
	maxLineSize   int

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
	relabelConfig []*relabel.Config,
	client client.APIClient,
	maxLineSize int,
) (*Target, error) {

	pos, err := position.Get(positions.CursorKey(containerName))
	if err != nil {
		return nil, err
	}
	var since int64
	if pos != 0 {
		since = pos
	}

	t := &Target{
		logger:        logger,
		handler:       handler,
		since:         since,
		positions:     position,
		containerName: containerName,
		labels:        labels,
		relabelConfig: relabelConfig,
		metrics:       metrics,
		maxLineSize:   maxLineSize,

		client:  client,
		running: atomic.NewBool(false),
	}
	t.startIfNotRunning()
	return t, nil
}

func (t *Target) processLoop(ctx context.Context) {
	t.running.Store(true)
	defer t.running.Store(false)

	t.wg.Add(1)
	defer t.wg.Done()

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Since:      strconv.FormatInt(t.since, 10),
	}
	inspectInfo, err := t.client.ContainerInspect(ctx, t.containerName)
	if err != nil {
		level.Error(t.logger).Log("msg", "could not inspect container info", "container", t.containerName, "err", err)
		t.err = err
		return
	}
	logs, err := t.client.ContainerLogs(ctx, t.containerName, opts)
	if err != nil {
		level.Error(t.logger).Log("msg", "could not fetch logs for container", "container", t.containerName, "err", err)
		t.err = err
		return
	}

	// Start transferring
	cstdout := make(chan []byte)
	cstderr := make(chan []byte)
	t.wg.Add(1)
	go func() {
		defer func() {
			t.wg.Done()
			close(cstdout)
			close(cstderr)
			t.Stop()
		}()
		var written int64
		var err error
		if inspectInfo.Config.Tty {
			written, err = framedstdcopy.NoHeaderFramedStdCopy(cstdout, logs)
		} else {
			written, err = framedstdcopy.FramedStdCopy(cstdout, cstderr, logs)
		}
		if err != nil {
			level.Warn(t.logger).Log("msg", "could not transfer logs", "written", written, "container", t.containerName, "err", err)
		} else {
			level.Info(t.logger).Log("msg", "finished transferring logs", "written", written, "container", t.containerName)
		}
	}()

	// Start processing
	t.wg.Add(2)
	go t.process(cstdout, "stdout")
	go t.process(cstderr, "stderr")

	// Wait until done
	<-ctx.Done()
	logs.Close()
	level.Debug(t.logger).Log("msg", "done processing Docker logs", "container", t.containerName)
}

// extractTs tries for read the timestamp from the beginning of the log line.
// It's expected to follow the format 2006-01-02T15:04:05.999999999Z07:00.
func extractTs(line string) (time.Time, string, error) {
	pair := strings.SplitN(line, " ", 2)
	if len(pair) != 2 {
		return time.Now(), line, fmt.Errorf("could not find timestamp in '%s'", line)
	}
	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", pair[0])
	if err != nil {
		return time.Now(), line, fmt.Errorf("could not parse timestamp from '%s': %w", pair[0], err)
	}
	return ts, pair[1], nil
}

func (t *Target) process(frames chan []byte, logStream string) {
	defer func() {
		t.wg.Done()
	}()

	var (
		sizeLimit            = t.maxLineSize
		discardRemainingLine = false
		payloadAcc           strings.Builder
		curTs                = time.Now()
	)

	// If max_line_size is disabled (set to 0), we can in theory have infinite buffer growth.
	// We can't guarantee that there's any bound on Docker logs, they could be an infinite stream
	// without newlines for all we know. To protect promtail from OOM in that case, we introduce
	// this safety limit into the Docker target, inspired by the default Loki max_line_size value:
	// https://grafana.com/docs/loki/latest/configure/#limits_config
	if sizeLimit == 0 {
		sizeLimit = 256 * 1024
	}

	for frame := range frames {
		// Split frame into timestamp and payload
		ts, payload, err := extractTs(string(frame))
		if err != nil {
			if payloadAcc.Len() == 0 {
				// If we are currently accumulating a line split over multiple frames, we would still expect
				// timestamps in every frame, but since we don't use those secondary ones, we don't log an error in that case.
				level.Error(t.logger).Log("msg", "error reading docker log line, skipping line", "err", err)
				t.metrics.dockerErrors.Inc()
				continue
			}
			ts = curTs
		}

		// If time has changed, we are looking at a new event (although we should have seen a new line..),
		// so flush the buffer if we have one.
		if ts != curTs {
			discardRemainingLine = false
			if payloadAcc.Len() > 0 {
				t.handleOutput(logStream, curTs, payloadAcc.String())
				payloadAcc.Reset()
			}
		}

		// Check if we have the end of the event
		var isEol = strings.HasSuffix(payload, "\n")

		// If we are currently discarding a line (due to size limits), skip ahead, but don't skip the next
		// frame if we saw the end of the line.
		if discardRemainingLine {
			discardRemainingLine = !isEol
			continue
		}

		// Strip newline ending if we have it
		payload = strings.TrimRight(payload, "\r\n")

		// Fast path: Most log lines are a single frame. If we have a full line in frame and buffer is empty,
		// then don't use the buffer at all.
		if payloadAcc.Len() == 0 && isEol {
			t.handleOutput(logStream, ts, payload)
			continue
		}

		// Add to buffer
		payloadAcc.WriteString(payload)
		curTs = ts

		// Send immediately if line ended or we built a very large event
		if isEol || payloadAcc.Len() > sizeLimit {
			discardRemainingLine = !isEol
			t.handleOutput(logStream, curTs, payloadAcc.String())
			payloadAcc.Reset()
		}
	}
}

func (t *Target) handleOutput(logStream string, ts time.Time, payload string) {
	// Add all labels from the config, relabel and filter them.
	lb := labels.NewBuilder(nil)
	for k, v := range t.labels {
		lb.Set(string(k), string(v))
	}
	lb.Set(dockerLabelLogStream, logStream)
	processed, _ := relabel.Process(lb.Labels(), t.relabelConfig...)

	filtered := make(model.LabelSet)
	for _, lbl := range processed {
		if strings.HasPrefix(lbl.Name, "__") {
			continue
		}
		filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	t.handler.Chan() <- api.Entry{
		Labels: filtered,
		Entry: logproto.Entry{
			Timestamp: ts,
			Line:      payload,
		},
	}
	t.metrics.dockerEntries.Inc()
	t.positions.Put(positions.CursorKey(t.containerName), ts.Unix())
	t.since = ts.Unix()
}

// startIfNotRunning starts processing container logs. The operation is idempotent , i.e. the processing cannot be started twice.
func (t *Target) startIfNotRunning() {
	if t.running.CompareAndSwap(false, true) {
		level.Debug(t.logger).Log("msg", "starting process loop", "container", t.containerName)
		ctx, cancel := context.WithCancel(context.Background())
		t.cancel = cancel
		go t.processLoop(ctx)
	} else {
		level.Debug(t.logger).Log("msg", "attempted to start process loop but it's already running", "container", t.containerName)
	}
}

func (t *Target) Stop() {
	t.cancel()
	t.wg.Wait()
	level.Debug(t.logger).Log("msg", "stopped Docker target", "container", t.containerName)
}

func (t *Target) Type() target.TargetType {
	return target.DockerTargetType
}

func (t *Target) Ready() bool {
	return t.running.Load()
}

func (t *Target) DiscoveredLabels() model.LabelSet {
	return t.labels
}

func (t *Target) Labels() model.LabelSet {
	return t.labels
}

// Details returns target-specific details.
func (t *Target) Details() interface{} {
	var errMsg string
	if t.err != nil {
		errMsg = t.err.Error()
	}
	return map[string]string{
		"id":       t.containerName,
		"error":    errMsg,
		"position": t.positions.GetString(positions.CursorKey(t.containerName)),
		"running":  strconv.FormatBool(t.running.Load()),
	}
}
