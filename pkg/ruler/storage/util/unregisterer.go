// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package util

import "github.com/prometheus/client_golang/prometheus"

// Unregisterer is a Prometheus Registerer that can unregister all collectors
// passed to it.
type Unregisterer struct {
	wrap prometheus.Registerer
	cs   map[prometheus.Collector]struct{}
}

// WrapWithUnregisterer wraps a prometheus Registerer with capabilities to
// unregister all collectors.
func WrapWithUnregisterer(reg prometheus.Registerer) *Unregisterer {
	return &Unregisterer{
		wrap: reg,
		cs:   make(map[prometheus.Collector]struct{}),
	}
}

// Register implements prometheus.Registerer.
func (u *Unregisterer) Register(c prometheus.Collector) error {
	if u.wrap == nil {
		return nil
	}

	err := u.wrap.Register(c)
	if err != nil {
		return err
	}
	u.cs[c] = struct{}{}
	return nil
}

// MustRegister implements prometheus.Registerer.
func (u *Unregisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := u.Register(c); err != nil {
			panic(err)
		}
	}
}

// Unregister implements prometheus.Registerer.
func (u *Unregisterer) Unregister(c prometheus.Collector) bool {
	if u.wrap != nil && u.wrap.Unregister(c) {
		delete(u.cs, c)
		return true
	}
	return false
}

// UnregisterAll unregisters all collectors that were registered through the
// Reigsterer.
func (u *Unregisterer) UnregisterAll() bool {
	success := true
	for c := range u.cs {
		if !u.Unregister(c) {
			success = false
		}
	}
	return success
}
