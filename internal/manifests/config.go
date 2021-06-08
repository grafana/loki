package manifests

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"github.com/ViaQ/loki-operator/internal/manifests/internal/config"
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
	return config.Options{
		Stack:     opt.Stack,
		Namespace: opt.Namespace,
		Name:      opt.Name,
		FrontendWorker: config.Address{
			FQDN: fqdn(NewQueryFrontendGRPCService(opt.Name).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		GossipRing: config.Address{
			FQDN: fqdn(BuildLokiGossipRingService(opt.Name).GetName(), opt.Namespace),
			Port: gossipPort,
		},
		Querier: config.Address{
			FQDN: fqdn(NewQuerierHTTPService(opt.Name).GetName(), opt.Namespace),
			Port: httpPort,
		},
		StorageDirectory: strings.TrimRight(dataDirectory, "/"),
		ObjectStorage: config.ObjectStorage{
			Endpoint:        opt.ObjectStorage.Endpoint,
			Buckets:         opt.ObjectStorage.Buckets,
			Region:          opt.ObjectStorage.Region,
			AccessKeyID:     opt.ObjectStorage.AccessKeyID,
			AccessKeySecret: opt.ObjectStorage.AccessKeySecret,
		},
		QueryParallelism: config.Parallelism{
			QuerierCPULimits:      opt.ResourceRequirements.Querier.Requests.Cpu().Value(),
			QueryFrontendReplicas: opt.Stack.Template.QueryFrontend.Replicas,
		},
	}
}

func lokiConfigMapName(stackName string) string {
	return fmt.Sprintf("loki-config-%s", stackName)
}
