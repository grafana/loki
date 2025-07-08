// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !localvalidationscheme

package push

import (
	"github.com/prometheus/common/model"

	"github.com/prometheus/client_golang/prometheus"
)

// New creates a new Pusher to push to the provided URL with the provided job
// name (which must not be empty). You can use just host:port or ip:port as url,
// in which case “http://” is added automatically. Alternatively, include the
// schema in the URL. However, do not include the “/metrics/jobs/…” part.
func New(url, job string) *Pusher {
	//nolint:staticcheck // model.NameValidationScheme is being phased out.
	return newPusher(url, job, model.NameValidationScheme)
}

func isLabelNameValid(labelName string, _ model.ValidationScheme) bool {
	return model.LabelName(labelName).IsValid()
}

func newGatherers(gatherers []prometheus.Gatherer, _ model.ValidationScheme) prometheus.Gatherers {
	return prometheus.NewGatherers(gatherers)
}

// Gatherer adds a Gatherer to the Pusher, from which metrics will be gathered
// to push them to the Pushgateway. The gathered metrics must not contain a job
// label of their own.
//
// For convenience, this method returns a pointer to the Pusher itself.
func (p *Pusher) Gatherer(g prometheus.Gatherer) *Pusher {
	p.gatherers = append(p.gatherers, g)
	return p
}
