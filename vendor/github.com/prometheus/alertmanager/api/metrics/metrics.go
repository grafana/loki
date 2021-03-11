// Copyright 2019 Prometheus Team
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

package metrics

import "github.com/prometheus/client_golang/prometheus"

// Alerts stores metrics for alerts which are common across all API versions.
type Alerts struct {
	firing   prometheus.Counter
	resolved prometheus.Counter
	invalid  prometheus.Counter
}

// NewAlerts returns an *Alerts struct for the given API version.
func NewAlerts(version string, r prometheus.Registerer) *Alerts {
	numReceivedAlerts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "alertmanager_alerts_received_total",
		Help:        "The total number of received alerts.",
		ConstLabels: prometheus.Labels{"version": version},
	}, []string{"status"})
	numInvalidAlerts := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "alertmanager_alerts_invalid_total",
		Help:        "The total number of received alerts that were invalid.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	if r != nil {
		r.MustRegister(numReceivedAlerts, numInvalidAlerts)
	}
	return &Alerts{
		firing:   numReceivedAlerts.WithLabelValues("firing"),
		resolved: numReceivedAlerts.WithLabelValues("resolved"),
		invalid:  numInvalidAlerts,
	}
}

// Firing returns a counter of firing alerts.
func (a *Alerts) Firing() prometheus.Counter { return a.firing }

// Resolved returns a counter of resolved alerts.
func (a *Alerts) Resolved() prometheus.Counter { return a.resolved }

// Invalid returns a counter of invalid alerts.
func (a *Alerts) Invalid() prometheus.Counter { return a.invalid }
