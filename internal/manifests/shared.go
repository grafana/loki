package manifests

import (
	"fmt"
	"strings"

	"github.com/ViaQ/loki-operator/internal/manifests/config"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func LokiConfigMap(stackName, namespace string) (*core.ConfigMap, error) {
	b, err := config.Build(config.Options{
		FrontendWorker: config.Address{
			FQDN: "",
			Port: 0,
		},
		GossipRing: config.Address{
			FQDN: fqdn(LokiGossipRingService(stackName).GetName(), namespace),
			Port: gossipPort,
		},
		Querier:          config.Address{
			FQDN: serviceNameQuerierHTTP(stackName),
			Port: httpPort,
		},
		StorageDirectory: strings.TrimRight(dataDirectory, "/"),
		Namespace:        namespace,
	})
	if err != nil {
		return nil, err
	}

	return &core.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   lokiConfigMapName(stackName),
			Labels: commonLabels(stackName),
		},
		BinaryData: map[string][]byte{
			config.LokiConfigFileName: b,
		},
	}, nil
}

func lokiConfigMapName(stackName string) string {
	return fmt.Sprintf("loki-config-%s", stackName)
}

func LokiGossipRingService(stackName string) *core.Service {
	return &core.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-gossip-ring-%s", stackName),
			Labels: commonLabels(stackName),
		},
		Spec: core.ServiceSpec{
			ClusterIP: "None",
			Ports: []core.ServicePort{
				{
					Name:     "gossip",
					Port:     gossipPort,
					Protocol: "TCP",
				},
			},
			Selector: commonLabels(stackName),
		},
	}
}
