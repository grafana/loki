package alerts

import (
	"bytes"
	// https://pkg.go.dev/embed#hdr-Strings_and_Bytes
	_ "embed"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed prometheus_alerts.yaml
var alertsBytes []byte

// NewAlertsSpec decodes the prometheus alerts for loki stack
func NewAlertsSpec() (*monitoringv1.PrometheusRuleSpec, error) {
	var alertsSpec *monitoringv1.PrometheusRuleSpec
	r := bytes.NewReader(alertsBytes)
	err := k8sYAML.NewYAMLOrJSONDecoder(r, 1000).Decode(&alertsSpec)
	if err != nil {
		return nil, err
	}
	return alertsSpec, nil
}
