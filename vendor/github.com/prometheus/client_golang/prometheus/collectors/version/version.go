// Copyright 2016 The Prometheus Authors
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

package version

import (
	"fmt"
	"maps"

	"github.com/prometheus/common/version"

	"github.com/prometheus/client_golang/prometheus"
)

type Option func(*options)

type options struct {
	extraConstLabels prometheus.Labels
}

func WithExtraConstLabels(l prometheus.Labels) Option {
	return func(o *options) {
		o.extraConstLabels = l
	}
}

// NewCollector returns a collector that exports metrics about current version
// information.
func NewCollector(program string, opts ...Option) prometheus.Collector {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	constLabels := prometheus.Labels{
		"version":   version.Version,
		"revision":  version.GetRevision(),
		"branch":    version.Branch,
		"goversion": version.GoVersion,
		"goos":      version.GoOS,
		"goarch":    version.GoArch,
		"tags":      version.GetTags(),
	}
	maps.Copy(constLabels, o.extraConstLabels)

	return prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: program,
			Name:      "build_info",
			Help: fmt.Sprintf(
				"A metric with a constant '1' value labeled by version, revision, branch, goversion from which %s was built, and the goos and goarch for the build.",
				program,
			),
			ConstLabels: constLabels,
		},
		func() float64 { return 1 },
	)
}
