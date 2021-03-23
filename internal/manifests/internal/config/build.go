package config

import (
	"bytes"
	"embed"
	"io/ioutil"
	"text/template"

	"github.com/ViaQ/logerr/kverrors"
)

const (
	// LokiConfigFileName is the name of the config file in the configmap
	LokiConfigFileName = "config.yaml"
	// LokiConfigMountDir is the path that is mounted from the configmap
	LokiConfigMountDir = "/etc/loki/config"
)

//go:embed loki-config.yaml
var lokiConfigYAMLTmplFile embed.FS

var lokiConfigYAMLTmpl = template.Must(template.ParseFS(lokiConfigYAMLTmplFile, "loki-config.yaml"))

// Build builds a loki stack configuration file
func Build(opts Options) ([]byte, error) {
	w := bytes.NewBuffer(nil)
	err := lokiConfigYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to create loki configuration")
	}
	b, err := ioutil.ReadAll(w)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	return b, nil
}
