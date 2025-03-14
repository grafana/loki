package rwrulefmt

import "github.com/prometheus/prometheus/model/rulefmt"

// Wrapper around Prometheus rulefmt.

// RuleGroup is a list of sequentially evaluated recording and alerting rules.
type RuleGroup struct {
	rulefmt.RuleGroup `yaml:",inline"`
	// RWConfigs is used by the remote write forwarding ruler
	RWConfigs []RemoteWriteConfig `yaml:"remote_write,omitempty"`
}

// RemoteWriteConfig is used to specify a remote write endpoint
type RemoteWriteConfig struct {
	URL string `json:"url,omitempty"`
}
