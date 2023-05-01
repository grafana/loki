package kubernetes

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"go.uber.org/atomic"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/pkg/logproto"
)

type Target struct {
	logger        log.Logger
	handler       api.EntryHandler
	since         int64
	positions     positions.Positions
	containerName string
	namespace     string
	pod           string
	cursorKey     string
	labels        model.LabelSet
	relabelConfig []*relabel.Config
	metrics       *Metrics

	cancel  context.CancelFunc
	client  kubernetes.Interface
	wg      sync.WaitGroup
	running *atomic.Bool
	err     error
}

func NewTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	position positions.Positions,
	namespace string,
	pod string,
	containerName string,
	labels model.LabelSet,
	relabelConfig []*relabel.Config,
	client kubernetes.Interface,
) (*Target, error) {

	cursorKey := positions.CursorKey(fmt.Sprintf("%s/%s/%s", namespace, pod, containerName))

	pos, err := position.Get(cursorKey)
	if err != nil {
		return nil, err
	}
	var since int64
	if pos != 0 {
		since = pos
	}

	t := &Target{
		logger:        log.With(logger, "container", containerName, "namespace", namespace, "pod", pod),
		handler:       handler,
		since:         since,
		positions:     position,
		namespace:     namespace,
		pod:           pod,
		containerName: containerName,
		labels:        labels,
		relabelConfig: relabelConfig,
		metrics:       metrics,
		cursorKey:     cursorKey,

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

	var since *metav1.Time
	if t.since != 0 {
		since = &metav1.Time{
			Time: time.UnixMicro(t.since),
		}
	}
	opts := v1.PodLogOptions{
		Container:  t.containerName,
		Follow:     true,
		SinceTime:  since,
		Timestamps: true,
	}
	logs, err := t.client.CoreV1().Pods(t.namespace).GetLogs(t.pod, &opts).Stream(ctx)
	if err != nil {
		level.Error(t.logger).Log("msg", "could not fetch logs for container", "err", err)
		t.err = err
		return
	}

	t.wg.Add(1)
	go t.process(ctx, logs)

	// Wait until done
	<-ctx.Done()
	logs.Close()
	level.Debug(t.logger).Log("msg", "done processing Kubernetes logs")
}

// extractTs tries for read the timestamp from the beginning of the log line.
// It's expected to follow the format 2006-01-02T15:04:05.999999999Z07:00.
func extractTs(line string) (time.Time, string, error) {
	tsPart, line, found := strings.Cut(line, " ")
	if !found {
		return time.Now(), line, fmt.Errorf("Could not find timestamp in '%s'", line)
	}
	ts, err := time.Parse(time.RFC3339, tsPart)
	if err != nil {
		return time.Now(), line, fmt.Errorf("Could not parse timestamp from '%s': %w", tsPart, err)
	}
	return ts, line, nil
}

// https://devmarkpro.com/working-big-files-golang
func readLine(r *bufio.Reader) (string, error) {
	var (
		isPrefix = true
		err      error
		line, ln []byte
	)

	for isPrefix && err == nil {
		line, isPrefix, err = r.ReadLine()
		ln = append(ln, line...)
	}

	return string(ln), err
}

func (t *Target) process(ctx context.Context, r io.Reader) {
	defer func() {
		t.wg.Done()
	}()

	reader := bufio.NewReader(r)
	for {
		if ctx.Err() != nil {
			break
		}
		line, err := readLine(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			level.Debug(t.logger).Log("msg", "error reading kubernetes log line, skipping line", "err", err)
			t.metrics.kubernetesErrors.Inc()
			continue
		}

		ts, line, err := extractTs(line)
		if err != nil {
			level.Debug(t.logger).Log("msg", "could not extract timestamp, skipping line", "err", err)
			t.metrics.kubernetesErrors.Inc()
			continue
		}

		// Add all labels from the config, relabel and filter them.
		lb := labels.NewBuilder(nil)
		for k, v := range t.labels {
			lb.Set(string(k), string(v))
		}
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
				Line:      line,
			},
		}
		t.metrics.kubernetesEntries.Inc()
		t.positions.Put(t.cursorKey, ts.UnixMicro())
	}
}

// startIfNotRunning starts processing container logs. The operation is idempotent , i.e. the processing cannot be started twice.
func (t *Target) startIfNotRunning() {
	if t.running.CompareAndSwap(false, true) {
		level.Debug(t.logger).Log("msg", "starting process loop")
		ctx, cancel := context.WithCancel(context.Background())
		t.cancel = cancel
		go t.processLoop(ctx)
	} else {
		level.Debug(t.logger).Log("msg", "attempted to start process loop but it's already running")
	}
}

func (t *Target) Stop() {
	t.cancel()
	t.wg.Wait()
	level.Debug(t.logger).Log("msg", "stopped Kubernetes target")
}

func (t *Target) Type() target.TargetType {
	return target.KubernetesTargetType
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
		"containerName": t.containerName,
		"namespace":     t.namespace,
		"pod":           t.pod,
		"error":         errMsg,
		"position":      t.positions.GetString(t.cursorKey),
		"running":       strconv.FormatBool(t.running.Load()),
	}
}
