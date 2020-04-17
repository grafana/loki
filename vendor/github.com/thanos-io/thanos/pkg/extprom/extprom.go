// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package extprom

import "github.com/prometheus/client_golang/prometheus"

// WrapRegistererWithPrefix is like prometheus.WrapRegistererWithPrefix but it passes nil straight through
// which allows nil check.
func WrapRegistererWithPrefix(prefix string, reg prometheus.Registerer) prometheus.Registerer {
	if reg == nil {
		return nil
	}
	return prometheus.WrapRegistererWithPrefix(prefix, reg)
}

// WrapRegistererWith is like prometheus.WrapRegistererWith but it passes nil straight through
// which allows nil check.
func WrapRegistererWith(labels prometheus.Labels, reg prometheus.Registerer) prometheus.Registerer {
	if reg == nil {
		return nil
	}
	return prometheus.WrapRegistererWith(labels, reg)
}
