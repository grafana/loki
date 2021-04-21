package manifests

import (
	"fmt"
	"strings"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/ViaQ/loki-operator/internal/manifests/internal"
	"github.com/ViaQ/loki-operator/internal/manifests/internal/config"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LokiConfigMap creates the single configmap containing the loki configuration for the whole cluster
func LokiConfigMap(opt Options) (*corev1.ConfigMap, error) {
	cfg, err := ConfigOptions(opt)
	if err != nil {
		return nil, err
	}

	b, err := config.Build(cfg)
	if err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   lokiConfigMapName(opt.Name),
			Labels: commonLabels(opt.Name),
		},
		BinaryData: map[string][]byte{
			config.LokiConfigFileName: b,
		},
	}, nil
}

// ConfigOptions converts Options to config.Options
func ConfigOptions(opt Options) (config.Options, error) {
	// First define the default values determined by our sizing table
	cfg := config.Options{
		Stack:     internal.StackSizeTable[opt.Stack.Size],
		Namespace: opt.Namespace,
		Name:      opt.Name,
		FrontendWorker: config.Address{
			FQDN: "",
			Port: 0,
		},
		GossipRing: config.Address{
			FQDN: fqdn(LokiGossipRingService(opt.Name).GetName(), opt.Namespace),
			Port: gossipPort,
		},
		Querier: config.Address{
			FQDN: serviceNameQuerierHTTP(opt.Name),
			Port: httpPort,
		},
		StorageDirectory: strings.TrimRight(dataDirectory, "/"),
	}

	// Now merge any configuration provided by the custom resource
	if err := mergo.Merge(&cfg.Stack, opt.Stack, mergo.WithOverride); err != nil {
		return config.Options{}, kverrors.Wrap(err, "failed to merge configs")
	}
	return cfg, nil
}

func lokiConfigMapName(stackName string) string {
	return fmt.Sprintf("loki-config-%s", stackName)
}
