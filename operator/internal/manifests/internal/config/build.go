package config

import (
	"bytes"
	"embed"
	"io"
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

	lokiConfigYAMLTmpl = template.Must(template.New("loki-config.yaml").Funcs(template.FuncMap{
		"schemas": func(specSchemas []lokiv1.ObjectStorageSchema) map[string]struct{} {
			schemas := map[string]struct{}{}
			for _, schema := range specSchemas {
				if schema.Version == lokiv1.ObjectStorageSchemaV11 || schema.Version == lokiv1.ObjectStorageSchemaV12 {
					schemas["boltdb"] = struct{}{}
				} else {
					schemas["tsdb"] = struct{}{}
				}
			}
			return schemas
		},
	}).ParseFS(lokiConfigYAMLTmplFile, "loki-config.yaml"))

	// lokiConfigYAMLTmpl = template.Must(template.ParseFS(lokiConfigYAMLTmplFile, "loki-config.yaml"))

	lokiRuntimeConfigYAMLTmpl = template.Must(template.ParseFS(lokiRuntimeConfigYAMLTmplFile, "loki-runtime-config.yaml"))
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
