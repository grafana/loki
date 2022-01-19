package alerts

import (
	"bytes"
	"embed"
	"io/ioutil"
	"text/template"

	"github.com/ViaQ/logerr/kverrors"
)

var (
	//go:embed prometheus_alerts.yaml.tmpl
	alertsTmplFile embed.FS

	//go:embed prometheus_recording_rules.yaml.tmpl
	rulesTmplFile embed.FS

	alertsTmpl = template.Must(template.New("prometheus_alerts.yaml.tmpl").Delims("[[", "]]").ParseFS(alertsTmplFile, "prometheus_alerts.yaml.tmpl"))
	rulesTmpl  = template.Must(template.New("prometheus_recording_rules.yaml.tmpl").Delims("[[", "]]").ParseFS(rulesTmplFile, "prometheus_recording_rules.yaml.tmpl"))
)

// Build builds prometheus recording rules and alerts for loki stack
func Build(opts Options) (alerts, rules []byte, err error) {
	alerts, err = executeTemplate(opts, alertsTmpl)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "Failed to execute alerts template")
	}

	rules, err = executeTemplate(opts, rulesTmpl)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "Failed to execute recording rules template")
	}

	return alerts, rules, nil
}

func executeTemplate(opts Options, tmpl *template.Template) (res []byte, err error) {
	buf := bytes.NewBuffer(nil)
	err = tmpl.Execute(buf, opts)
	if err != nil {
		return nil, kverrors.Wrap(err, "Failed to execute template")
	}
	res, err = ioutil.ReadAll(buf)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to read template output from buffer")
	}
	return res, nil
}
