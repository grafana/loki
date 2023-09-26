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
	RunbookDefaultURL = "https://github.com/grafana/loki/blob/main/operator/docs/lokistack/sop.md"
)

var (
	//go:embed prometheus-alerts.yaml
	alertsYAMLTmplFile embed.FS

	//go:embed prometheus-rules.yaml
	rulesYAMLTmplFile embed.FS

	alertsYAMLTmpl = template.Must(template.New("").Delims("[[", "]]").ParseFS(alertsYAMLTmplFile, "prometheus-alerts.yaml"))

	rulesYAMLTmpl = template.Must(template.New("").Delims("[[", "]]").ParseFS(rulesYAMLTmplFile, "prometheus-rules.yaml"))
)

// Build creates Prometheus alerts for the Loki stack
func Build(opts Options) (*monitoringv1.PrometheusRuleSpec, error) {
	alerts, err := ruleSpec("prometheus-alerts.yaml", alertsYAMLTmpl, opts)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to create prometheus alerts")
	}

	recordingRules, err := ruleSpec("prometheus-rules.yaml", rulesYAMLTmpl, opts)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to create prometheus rules")
	}

	spec := alerts.DeepCopy()
	spec.Groups = append(alerts.Groups, recordingRules.Groups...)

	return spec, nil
}

func ruleSpec(file string, tmpl *template.Template, opts Options) (*monitoringv1.PrometheusRuleSpec, error) {
	spec := monitoringv1.PrometheusRuleSpec{}

	w := bytes.NewBuffer(nil)
	err := tmpl.ExecuteTemplate(w, file, opts)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to execute template",
			"template", file,
		)
	}

	r := io.Reader(w)
	err = yaml.NewYAMLOrJSONDecoder(r, 1000).Decode(&spec)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to decode spec from reader")
	}

	return &spec, nil
}
