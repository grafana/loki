package alerts

import lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"

// Options is used to render the prometheus_alerts.yaml.tmpl and prometheus_recording_rules.yaml.tmpl file templates
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

	WritePathHighLoadThreshold float64
	ReadPathHighLoadThreshold  float64
}
