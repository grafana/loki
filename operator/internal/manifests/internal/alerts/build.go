package alerts

import (
	"bytes"
	// https://pkg.go.dev/embed#hdr-Strings_and_Bytes
	_ "embed"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	//go:embed prometheus_alerts.yaml
	alertsBytes []byte

	alertsRules monitoringv1.PrometheusRuleSpec
)

func init() {
	r := bytes.NewReader(alertsBytes)
	err := yaml.NewYAMLOrJSONDecoder(r, 1000).Decode(&alertsRules)
	if err != nil {
		panic(err)
	}
}

// NewAlertsSpec decodes the prometheus alerts for loki stack
func NewAlertsSpec() monitoringv1.PrometheusRuleSpec {
	return alertsRules
}
