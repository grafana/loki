package manifests

import (
	"crypto/sha1"
	"fmt"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LokiConfigMap creates the single configmap containing the loki configuration for the whole cluster
func LokiConfigMap(opt Options) (*corev1.ConfigMap, string, error) {
	cfg := ConfigOptions(opt)
	c, rc, err := config.Build(cfg)
	if err != nil {
		return nil, "", err
	}

	s := sha1.New()
	_, err = s.Write(c)
	if err != nil {
		return nil, "", err
	}
	sha1C := fmt.Sprintf("%x", s.Sum(nil))

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
			config.LokiConfigFileName:        c,
			config.LokiRuntimeConfigFileName: rc,
		},
	}, sha1C, nil
}

// ConfigOptions converts Options to config.Options
func ConfigOptions(opt Options) config.Options {
	rulerEnabled := opt.Stack.Rules != nil && opt.Stack.Rules.Enabled

	return config.Options{
		Stack:     opt.Stack,
		Namespace: opt.Namespace,
		Name:      opt.Name,
		FrontendWorker: config.Address{
			FQDN: fqdn(NewQueryFrontendGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		GossipRing: config.Address{
			FQDN: fqdn(BuildLokiGossipRingService(opt.Name).GetName(), opt.Namespace),
			Port: gossipPort,
		},
		Querier: config.Address{
			FQDN: fqdn(NewQuerierHTTPService(opt).GetName(), opt.Namespace),
			Port: httpPort,
		},
		IndexGateway: config.Address{
			FQDN: fqdn(NewIndexGatewayGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		StorageDirectory: dataDirectory,
		MaxConcurrent: config.MaxConcurrent{
			AvailableQuerierCPUCores: int32(opt.ResourceRequirements.Querier.Requests.Cpu().Value()),
		},
		WriteAheadLog: config.WriteAheadLog{
			Directory:             walDirectory,
			IngesterMemoryRequest: opt.ResourceRequirements.Ingester.Requests.Memory().Value(),
		},
		ObjectStorage:         opt.ObjectStorage,
		EnableRemoteReporting: opt.Flags.EnableGrafanaLabsStats,
		Ruler: config.Ruler{
			Enabled:               rulerEnabled,
			RulesStorageDirectory: rulesStorageDirectory,
		},
	}
}

func lokiConfigMapName(stackName string) string {
	return fmt.Sprintf("%s-config", stackName)
}
