package manifests

import (
	"fmt"

	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LokiGossipRingService creates a k8s service for the gossip/memberlist members of the cluster
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
