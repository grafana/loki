package kubernetes

import (
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

// targetGroup manages all container targets of one Kubernetes service.
type targetGroup struct {
	metrics       *Metrics
	logger        log.Logger
	positions     positions.Positions
	entryHandler  api.EntryHandler
	defaultLabels model.LabelSet
	relabelConfig []*relabel.Config
	client        kubernetes.Interface

	mtx     sync.Mutex
	targets map[string]*Target
}

func (tg *targetGroup) sync(groups []*targetgroup.Group) {
	tg.mtx.Lock()
	defer tg.mtx.Unlock()

	for _, group := range groups {
		for _, t := range group.Targets {
			namespace, ok := group.Labels[kubernetesLabelNamespace]
			if !ok {
				level.Warn(tg.logger).Log("msg", "Kubernetes target did not include namespace")
				continue
			}

			podName, ok := group.Labels[kubernetesLabelPodName]
			if !ok {
				level.Warn(tg.logger).Log("msg", "Kubernetes target did not include pod name")
				continue
			}

			containerName, ok := t[kubernetesLabelContainerName]
			if !ok {
				level.Warn(tg.logger).Log("msg", "Kubernetes target did not include container name")
				continue
			}

			err := tg.addTarget(string(namespace), string(podName), string(containerName), t)
			if err != nil {
				level.Error(tg.logger).Log("msg", "could not add target", "podName", podName, "namespace", namespace, "containerName", containerName, "err", err)
			}
		}
	}
}

// addTarget checks whether the container with given id is already known. If not it's added to the this group
func (tg *targetGroup) addTarget(namespace, podName, containerName string, discoveredLabels model.LabelSet) error {
	targetId := fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)
	if t, ok := tg.targets[targetId]; ok {
		level.Debug(tg.logger).Log("msg", "container target already exists", "namespace", namespace, "pod", podName, "container", containerName)
		t.startIfNotRunning()
		return nil
	}

	t, err := NewTarget(
		tg.metrics,
		log.With(tg.logger, "target", fmt.Sprintf("kubernetes/%s", targetId)),
		tg.entryHandler,
		tg.positions,
		namespace,
		podName,
		containerName,
		discoveredLabels.Merge(tg.defaultLabels),
		tg.relabelConfig,
		tg.client,
	)
	if err != nil {
		return err
	}
	tg.targets[targetId] = t
	level.Info(tg.logger).Log("msg", "added Kubernetes target", "namespace", namespace, "pod", podName, "container", containerName)
	return nil
}

// Ready returns true if at least one target is running.
func (tg *targetGroup) Ready() bool {
	tg.mtx.Lock()
	defer tg.mtx.Unlock()

	for _, t := range tg.targets {
		if t.Ready() {
			return true
		}
	}

	return true
}

// Stop all targets
func (tg *targetGroup) Stop() {
	tg.mtx.Lock()
	defer tg.mtx.Unlock()

	for _, t := range tg.targets {
		t.Stop()
	}
	tg.entryHandler.Stop()
}

// ActiveTargets return all targets that are ready.
func (tg *targetGroup) ActiveTargets() []target.Target {
	tg.mtx.Lock()
	defer tg.mtx.Unlock()

	result := make([]target.Target, 0, len(tg.targets))
	for _, t := range tg.targets {
		if t.Ready() {
			result = append(result, t)
		}
	}
	return result
}

// AllTargets returns all targets of this group.
func (tg *targetGroup) AllTargets() []target.Target {
	result := make([]target.Target, 0, len(tg.targets))
	for _, t := range tg.targets {
		result = append(result, t)
	}
	return result
}
