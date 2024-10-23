package config

import (
	"errors"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
)

var errConfigFileLoading = errors.New("could not read file at path")

func loadConfigFile(scheme *runtime.Scheme, configFile string) (*configv1.ProjectConfig, error) {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("%w %s", errConfigFileLoading, configFile)
	}

	codecs := serializer.NewCodecFactory(scheme)

	outConfig := &configv1.ProjectConfig{}
	if err = runtime.DecodeInto(codecs.UniversalDecoder(), content, outConfig); err != nil {
		return nil, fmt.Errorf("could not decode file into runtime.Object: %w", err)
	}

	return outConfig, nil
}
