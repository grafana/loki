package docker

import (
	"fmt"

	"github.com/docker/docker/client"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

type syncer struct {
	metrics       *Metrics
	logger        log.Logger
	positions     positions.Positions
	entryHandler  api.EntryHandler
	targets       map[string]*Target
	defaultLabels model.LabelSet
	host          string
	port          int
}

func (s *syncer) sync(groups []*targetgroup.Group) {
	level.Debug(s.logger).Log("msg", "synchronize groups")
	for _, group := range groups {
		if group.Source != "Docker" {
			continue
		}

		for _, t := range group.Targets {
			containerID, ok := t[dockerLabelContainerID]
			if !ok {
				level.Debug(s.logger).Log("msg", "target did not include container ID")
				continue
			}

			s.addTarget(string(containerID), t)
		}
	}
}

func (s *syncer) addTarget(id string, labels model.LabelSet) error {
	// TODO: load client options from config
	client, err := client.NewClientWithOpts(client.WithHost(s.host))
	if err != nil {
		level.Error(s.logger).Log("msg", "could not create new Docker client", "err", err)
		return err
	}

	_, ok := s.targets[id]
	if ok {
		return nil
	}

	t, err := NewTarget(
		s.metrics,
		log.With(s.logger, "target", fmt.Sprintf("docker/%s", id)),
		s.entryHandler,
		s.positions,
		id,
		labels.Merge(s.defaultLabels),
		client,
	)
	if err != nil {
		return err
	}
	s.targets[id] = t
	return nil
}

// Ready returns true if at least one target is running.
func (s *syncer) Ready() bool {
	for _, t := range s.targets {
		if t.Ready() {
			return true
		}
	}

	return true
}

func (s *syncer) Stop() {
	for _, t := range s.targets {
		t.Stop()
	}
}

func (s *syncer) ActiveTargets() []target.Target {
	result := make([]target.Target, 0, len(s.targets))
	for _, t := range s.targets {
		if t.Ready() {
			result = append(result, t)
		}
	}
	return result
}
func (s *syncer) AllTargets() []target.Target {
	result := make([]target.Target, 0, len(s.targets))
	for _, t := range s.targets {
		result = append(result, t)
	}
	return result
}
