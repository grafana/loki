package config

import (
	"bytes"
	"embed"
	"io"
	"strings"
	"text/template"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

const (
	// LokiConfigFileName is the name of the config file in the configmap
	LokiConfigFileName = "config.yaml"
	// LokiRuntimeConfigFileName is the name of the runtime config file in the configmap
	LokiRuntimeConfigFileName = "runtime-config.yaml"
	// LokiConfigMountDir is the path that is mounted from the configmap
	LokiConfigMountDir = "/etc/loki/config"
)

var (
	//go:embed loki-config.yaml
	lokiConfigYAMLTmplFile embed.FS

	//go:embed loki-runtime-config.yaml
	lokiRuntimeConfigYAMLTmplFile embed.FS

	lokiConfigYAMLTmpl = template.Must(template.ParseFS(lokiConfigYAMLTmplFile, "loki-config.yaml"))

	lokiRuntimeConfigYAMLTmpl = template.Must(template.New("loki-runtime-config.yaml").Funcs(template.FuncMap{
		"blockedQueryTypesToString": blockedQueryTypesToString,
	}).ParseFS(lokiRuntimeConfigYAMLTmplFile, "loki-runtime-config.yaml"))
)

// Build builds a loki stack configuration files
func Build(opts Options) ([]byte, []byte, error) {
	// Build loki config yaml
	w := bytes.NewBuffer(nil)
	err := lokiConfigYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to create loki configuration")
	}
	cfg, err := io.ReadAll(w)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	// Build loki runtime config yaml
	w = bytes.NewBuffer(nil)
	err = lokiRuntimeConfigYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to create loki runtime configuration")
	}
	rcfg, err := io.ReadAll(w)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	return cfg, rcfg, nil
}

func blockedQueryTypesToString(ts []lokiv1.BlockedQueryType) string {
	var s []string
	for _, t := range ts {
		s = append(s, string(t))
	}
	return strings.Join(s, ",")
}
