package alerts

import (
	"bytes"
	"embed"
	"io"
	"text/template"

	"github.com/ViaQ/logerr/v2/kverrors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	// RunbookDefaultURL is the default url for the documentation of the Prometheus alerts
	RunbookDefaultURL = "https://github.com/grafana/loki/tree/main/operator/docs/alerts.md"
)

var (
	//go:embed prometheus-alerts.yaml
	alertsYAMLTmplFile embed.FS

	alertsYAMLTmpl = template.Must(template.New("").Delims("[[", "]]").ParseFS(alertsYAMLTmplFile, "prometheus-alerts.yaml"))
)

// Build creates Prometheus alerts for the Loki stack
func Build(opts Options) (*monitoringv1.PrometheusRuleSpec, error) {
	spec := monitoringv1.PrometheusRuleSpec{}

	// Build alerts yaml
	w := bytes.NewBuffer(nil)
	err := alertsYAMLTmpl.ExecuteTemplate(w, "prometheus-alerts.yaml", opts)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to create prometheus alerts")
	}

	// Decode the spec
	r := io.Reader(w)
	err = yaml.NewYAMLOrJSONDecoder(r, 1000).Decode(&spec)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to decode spec from reader")
	}

	return &spec, nil
}
